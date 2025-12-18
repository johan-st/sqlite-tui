package history

import (
	"fmt"
	"math/rand"
	"time"
)

// adjectives for anonymous name generation
var adjectives = []string{
	"azure", "crimson", "emerald", "golden", "silver", "violet", "amber", "coral",
	"indigo", "jade", "onyx", "pearl", "ruby", "sapphire", "topaz", "bronze",
	"copper", "ivory", "obsidian", "opal", "crystal", "ebony", "frost", "storm",
	"shadow", "lunar", "solar", "stellar", "cosmic", "mystic", "arctic", "autumn",
	"spring", "summer", "winter", "misty", "silent", "swift", "brave", "clever",
	"gentle", "noble", "proud", "wild", "calm", "bold", "bright", "dark",
}

// animals for anonymous name generation
var animals = []string{
	"tiger", "falcon", "wolf", "eagle", "bear", "hawk", "lion", "panther",
	"phoenix", "dragon", "raven", "fox", "deer", "owl", "crane", "dolphin",
	"otter", "badger", "heron", "sparrow", "condor", "jaguar", "leopard", "lynx",
	"puma", "cobra", "viper", "python", "tortoise", "turtle", "salmon", "trout",
	"shark", "whale", "seal", "penguin", "pelican", "flamingo", "parrot", "finch",
	"cardinal", "robin", "jay", "wren", "swift", "martin", "oriole", "thrush",
}

// NameGenerator generates unique anonymous names.
type NameGenerator struct {
	rng *rand.Rand
}

// NewNameGenerator creates a new name generator.
func NewNameGenerator() *NameGenerator {
	return &NameGenerator{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Generate creates a new anonymous name in the format "adjective-animal-number".
func (g *NameGenerator) Generate() string {
	adj := adjectives[g.rng.Intn(len(adjectives))]
	animal := animals[g.rng.Intn(len(animals))]
	num := g.rng.Intn(100)
	return fmt.Sprintf("%s-%s-%02d", adj, animal, num)
}

// GenerateWithSeed creates a deterministic name from a seed (useful for consistent naming).
func GenerateWithSeed(seed int64) string {
	rng := rand.New(rand.NewSource(seed))
	adj := adjectives[rng.Intn(len(adjectives))]
	animal := animals[rng.Intn(len(animals))]
	num := rng.Intn(100)
	return fmt.Sprintf("%s-%s-%02d", adj, animal, num)
}
