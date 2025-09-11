package config

import (
    "os"
    "strconv"
    "strings"
)

type CredMode string

const (
    CredModeHost CredMode = "host"
    CredModePool CredMode = "pool"
)

type Strategy string

const (
    StrategyByIndex Strategy = "by_index"
    StrategyShuffle Strategy = "shuffle"
)

// PoolConfig holds opt-in configuration for the credential pool.
type PoolConfig struct {
    Mode     CredMode
    Dir      string
    Strategy Strategy
    Seed     int // used only for shuffle; 0 means random
}

// ReadPoolConfig parses environment variables into a PoolConfig.
// Defaults preserve current behavior (host mode).
func ReadPoolConfig() PoolConfig {
    mode := CredMode(strings.ToLower(strings.TrimSpace(os.Getenv("DEVKIT_CODEX_CRED_MODE"))))
    if mode == "" { mode = CredModeHost }
    if mode != CredModeHost && mode != CredModePool { mode = CredModeHost }

    strat := Strategy(strings.ToLower(strings.TrimSpace(os.Getenv("DEVKIT_CODEX_POOL_STRATEGY"))))
    if strat == "" { strat = StrategyByIndex }
    if strat != StrategyByIndex && strat != StrategyShuffle { strat = StrategyByIndex }

    dir := strings.TrimSpace(os.Getenv("DEVKIT_CODEX_POOL_DIR"))

    var seed int
    if s := strings.TrimSpace(os.Getenv("DEVKIT_CODEX_POOL_SEED")); s != "" {
        if v, err := strconv.Atoi(s); err == nil { seed = v }
    }

    return PoolConfig{Mode: mode, Dir: dir, Strategy: strat, Seed: seed}
}

