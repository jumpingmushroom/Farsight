package worldgen

type Biome uint16

const (
	Meadows     Biome = 1
	Swamp       Biome = 2
	Mountain    Biome = 4
	BlackForest Biome = 8
	Plains      Biome = 16
	AshLands    Biome = 32
	DeepNorth   Biome = 64
	Ocean       Biome = 256
	Mistlands   Biome = 512
)

func (b Biome) String() string {
	switch b {
	case Meadows:
		return "Meadows"
	case Swamp:
		return "Swamp"
	case Mountain:
		return "Mountain"
	case BlackForest:
		return "BlackForest"
	case Plains:
		return "Plains"
	case AshLands:
		return "AshLands"
	case DeepNorth:
		return "DeepNorth"
	case Ocean:
		return "Ocean"
	case Mistlands:
		return "Mistlands"
	}
	return "None"
}
