/*
 * Playwright global setup: a real, seeded `farsight serve` for the e2e suite.
 *
 *  1. Binaries. `farsight` embeds the web build (-tags webui). When
 *     FARSIGHT_BIN names a prebuilt one (`make e2e` builds it first) it is
 *     used as is; otherwise `npm run build` runs and farsight is built into
 *     the temp dir. `farsight-seed` is always built there (cached by go).
 *  2. A fresh temp dir under the OS temp dir holds the config, data dir,
 *     server log and binaries. The config has `demo` (crossplay, address,
 *     Discord hint, max 10) and `quiet` (Steam only, never seeded), with
 *     passphrases and agent tokens hashed by `farsight hash`, cookieSecure
 *     false, and a free 127.0.0.1 port.
 *  3. `farsight-seed -fake-tiles` writes a complete tile set before serve
 *     starts, so the snapshot post finds the map ready; serve starts, /healthz
 *     is awaited, then demo's snapshot and events are posted with -shift (the
 *     newest event lands at now − 1 min) and -explored (the snapshot carries
 *     a 12 m explored mask rasterised from its zones, as a current agent's).
 *  4. Keep-alive: heartbeats go stale 3 min after the last one, and a re-seed
 *     can't refresh them (events dedupe by id). A timer in this (runner)
 *     process posts one fresh heartbeat for demo at once and then every 30 s,
 *     each with a unique id, gzip + bearer like the agent. The token comes
 *     from FARSIGHT_SEED_TOKEN, the same variable farsight-seed reads.
 *  5. Teardown (the returned function, which Playwright runs after the tests
 *     whatever their outcome): stop the timer, SIGTERM the server (SIGKILL
 *     after 5 s), remove the temp dir. A failed setup cleans up the same way
 *     before rethrowing, and a process 'exit' hook kills the server as a last
 *     resort if the runner dies without running teardown.
 *
 * Tests read the base URL from process.env.BASE_URL (see helpers.ts).
 */
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { closeSync, mkdtempSync, openSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';

const WEB = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const REPO = resolve(WEB, '..');
const FIXTURES = join(WEB, 'tests/fixtures');

const HEARTBEAT_EVERY_MS = 30_000;
const HEALTH_TIMEOUT_MS = 20_000;

function run(cmd: string, args: string[], opts: { cwd?: string; input?: string; env?: NodeJS.ProcessEnv } = {}): string {
	return execFileSync(cmd, args, {
		cwd: opts.cwd ?? REPO,
		input: opts.input,
		env: opts.env ?? process.env,
		encoding: 'utf8',
		stdio: [opts.input === undefined ? 'ignore' : 'pipe', 'pipe', 'pipe']
	});
}

function freePort(): Promise<number> {
	return new Promise((res, rej) => {
		const srv = createServer();
		srv.unref();
		srv.on('error', rej);
		srv.listen(0, '127.0.0.1', () => {
			const addr = srv.address();
			srv.close(() => (typeof addr === 'object' && addr ? res(addr.port) : rej(new Error('no port'))));
		});
	});
}

async function waitHealthy(base: string, proc: ChildProcess, log: string): Promise<void> {
	const deadline = Date.now() + HEALTH_TIMEOUT_MS;
	while (Date.now() < deadline) {
		if (proc.exitCode !== null) {
			throw new Error(`farsight serve exited (${proc.exitCode}):\n${readFileSync(log, 'utf8')}`);
		}
		try {
			const r = await fetch(`${base}/healthz`);
			if (r.ok) return;
		} catch {
			// Not listening yet.
		}
		await new Promise((r) => setTimeout(r, 100));
	}
	throw new Error(`farsight serve not healthy after ${HEALTH_TIMEOUT_MS} ms:\n${readFileSync(log, 'utf8')}`);
}

async function postHeartbeat(base: string, token: string, n: number): Promise<void> {
	const body = gzipSync(
		JSON.stringify({
			events: [{ id: `e2e-heartbeat-${process.pid}-${Date.now()}-${n}`, type: 'heartbeat', at: new Date().toISOString() }]
		})
	);
	const r = await fetch(`${base}/ingest/demo/events`, {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${token}`,
			'Content-Type': 'application/json',
			'Content-Encoding': 'gzip'
		},
		body
	});
	if (!r.ok) throw new Error(`heartbeat: status ${r.status}: ${await r.text()}`);
}

function stopServer(proc: ChildProcess | undefined): Promise<void> {
	if (!proc || proc.exitCode !== null || proc.signalCode !== null) return Promise.resolve();
	return new Promise((res) => {
		const kill = setTimeout(() => proc.kill('SIGKILL'), 5_000);
		proc.once('exit', () => {
			clearTimeout(kill);
			res();
		});
		proc.kill('SIGTERM');
	});
}

export default async function globalSetup(): Promise<() => Promise<void>> {
	const dir = mkdtempSync(join(tmpdir(), 'farsight-e2e-'));
	let proc: ChildProcess | undefined;
	let timer: ReturnType<typeof setInterval> | undefined;
	const onExit = () => {
		if (proc && proc.exitCode === null) proc.kill('SIGKILL');
	};
	process.on('exit', onExit);

	const cleanup = async () => {
		if (timer) clearInterval(timer);
		await stopServer(proc);
		process.off('exit', onExit);
		rmSync(dir, { recursive: true, force: true });
	};

	try {
		// 1. Binaries.
		let farsight = process.env.FARSIGHT_BIN ? resolve(process.env.FARSIGHT_BIN) : '';
		if (!farsight) {
			run('npm', ['run', 'build'], { cwd: WEB });
			farsight = join(dir, 'farsight');
			run('go', ['build', '-tags', 'webui', '-o', farsight, './cmd/farsight']);
		}
		const seed = join(dir, 'farsight-seed');
		run('go', ['build', '-o', seed, './cmd/farsight-seed']);

		// 2. Config.
		const hash = (secret: string) => run(farsight, ['hash'], { input: `${secret}\n` }).trim();
		const port = await freePort();
		const base = `http://127.0.0.1:${port}`;
		const data = join(dir, 'data');
		const config = {
			listen: `127.0.0.1:${port}`,
			dataDir: data,
			cookieSecure: false,
			servers: [
				{
					id: 'demo',
					name: 'Demo',
					crossplay: true,
					address: 'play.example.net:2456',
					discordHint: 'ask in #demo',
					maxPlayers: 10,
					passphraseHash: hash('demo-pass'),
					agentTokenHash: hash('demo-token')
				},
				{
					id: 'quiet',
					name: 'Quiet Fjord',
					crossplay: false,
					maxPlayers: 10,
					passphraseHash: hash('quiet-pass'),
					agentTokenHash: hash('quiet-token')
				}
			]
		};
		const configPath = join(dir, 'farsight.json');
		writeFileSync(configPath, JSON.stringify(config, null, 2));

		// 3. Tiles, serve, seed.
		const snapshot = join(FIXTURES, 'snapshot.json');
		const events = join(FIXTURES, 'events.json');
		run(seed, ['-fake-tiles', data, '-snapshot', snapshot]);

		const log = join(dir, 'serve.log');
		const fd = openSync(log, 'a');
		proc = spawn(farsight, ['serve', '-config', configPath], {
			env: { ...process.env, FARSIGHT_COOKIE_KEY: randomBytes(32).toString('base64') },
			stdio: ['ignore', fd, fd]
		});
		closeSync(fd);
		await waitHealthy(base, proc, log);

		process.env.FARSIGHT_SEED_TOKEN = 'demo-token';
		run(seed, ['-url', base, '-server', 'demo', '-shift', '-explored', '-snapshot', snapshot, '-events', events]);

		// 4. Keep-alive.
		let n = 0;
		const beat = () =>
			postHeartbeat(base, process.env.FARSIGHT_SEED_TOKEN!, n++).catch((err) =>
				console.error('e2e keep-alive:', err instanceof Error ? err.message : err)
			);
		await beat();
		timer = setInterval(beat, HEARTBEAT_EVERY_MS);
		// Node's timer (typed as the DOM's number here): never keep the runner alive.
		(timer as unknown as { unref(): void }).unref();

		process.env.BASE_URL = base;
		return cleanup;
	} catch (err) {
		await cleanup();
		throw err;
	}
}
