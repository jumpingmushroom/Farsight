// The few Node.js APIs the e2e harness (global-setup.ts, helpers.ts) uses,
// declared here so `npm run check` type-checks it without adding
// @types/node (the plan's dependency list is closed). Playwright runs these
// files under Node itself; only the typing is local. Delete this file if
// @types/node is ever added.

declare namespace NodeJS {
	type ProcessEnv = Record<string, string | undefined>;
}

declare const process: {
	env: NodeJS.ProcessEnv;
	pid: number;
	on(event: 'exit', listener: () => void): void;
	off(event: 'exit', listener: () => void): void;
};

declare module 'node:child_process' {
	type Signal = 'SIGTERM' | 'SIGKILL' | 'SIGINT';
	export interface ChildProcess {
		exitCode: number | null;
		signalCode: Signal | null;
		kill(signal?: Signal): boolean;
		once(event: 'exit', listener: () => void): this;
	}
	export function execFileSync(
		file: string,
		args: string[],
		options: {
			cwd?: string;
			input?: string;
			env?: NodeJS.ProcessEnv;
			encoding: 'utf8';
			stdio?: ('ignore' | 'pipe')[];
		}
	): string;
	export function spawn(
		command: string,
		args: string[],
		options: { env?: NodeJS.ProcessEnv; stdio?: ('ignore' | 'pipe' | number)[] }
	): ChildProcess;
}

declare module 'node:crypto' {
	export function randomBytes(n: number): { toString(encoding: 'base64' | 'hex'): string };
}

declare module 'node:fs' {
	export function mkdtempSync(prefix: string): string;
	export function openSync(path: string, flags: string): number;
	export function closeSync(fd: number): void;
	export function readFileSync(path: string, encoding: 'utf8'): string;
	export function writeFileSync(path: string, data: string): void;
	export function rmSync(path: string, options: { recursive?: boolean; force?: boolean }): void;
}

declare module 'node:net' {
	export interface Server {
		unref(): this;
		on(event: 'error', listener: (err: Error) => void): this;
		listen(port: number, host: string, listener: () => void): this;
		address(): { port: number } | string | null;
		close(callback?: () => void): this;
	}
	export function createServer(): Server;
}

declare module 'node:os' {
	export function tmpdir(): string;
}

declare module 'node:path' {
	export function dirname(p: string): string;
	export function join(...parts: string[]): string;
	export function resolve(...parts: string[]): string;
}

declare module 'node:url' {
	export function fileURLToPath(url: string | URL): string;
}

declare module 'node:zlib' {
	export function gzipSync(data: string): Uint8Array<ArrayBuffer>;
}
