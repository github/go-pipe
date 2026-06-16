package pipe

import "context"

// Config represents a configuration for running pipes. A config
// instance is immutable. If you want a new configuration, either use
// `NewConfig()` to create one from scratch, or call
// `cfg.WithOption()` or `cfg.WithOptions()` to derive a new
// configuration from an existing one by adding more options. `Config`
// itself implements `Option` and `ConfigOption`, so it can be used
// when constructing a `Pipeline`. Or the Pipeline can be created
// using the helper method `Config.NewPipeline()`.
type Config struct {
	options sliceConfigOption
}

func newConfig(options []ConfigOption, extraSlots int) *Config {
	cfg := Config{
		options: make(sliceConfigOption, 0, len(options)+extraSlots),
	}
	cfg.options = append(cfg.options, options...)
	return &cfg
}

// NewConfig returns a `Config` with the specified `options`.
func NewConfig(options ...ConfigOption) *Config {
	return newConfig(options, 0)
}

// WithOption returns a new `Config` with the same options as `cfg`
// plus the additional `option`.
func (cfg *Config) WithOption(option ConfigOption) *Config {
	newCfg := newConfig(cfg.options, 1)
	newCfg.options = append(newCfg.options, option)
	return newCfg
}

// WithOption returns a new `Config` with the same options as `cfg`
// plus the additional `options`.
func (cfg *Config) WithOptions(options ...ConfigOption) *Config {
	newCfg := newConfig(cfg.options, len(options))
	newCfg.options = append(newCfg.options, options...)
	return newCfg
}

func (cfg *Config) newRunner(stage Stage, options ...Option) *runner {
	r := newRunner(stage, cfg.options)
	r.applyOptions(options...)
	return r
}

func (cfg *Config) NewPipeline(options ...Option) *Pipeline {
	return New(cfg.options, sliceOption(options))
}

// Start starts `stage` using a runner created using `cfg` plus any
// additional start options from `options`. If `Start()` exits without
// an error, the returned `Waiter` must also be called, to learn about
// any errors and to ensure that all resources are freed. See
// [runner.Start] for more information.
func (cfg *Config) Start(
	ctx context.Context, stage Stage, options ...Option,
) (WaitFunc, error) {
	return cfg.newRunner(stage, options...).start(ctx)
}

// Start starts `stage` using a runner created using `cfg` plus any
// additional start options from `options`.
func (cfg *Config) Run(
	ctx context.Context, stage Stage, options ...Option,
) error {
	return cfg.newRunner(stage, options...).run(ctx)
}

func (cfg *Config) Output(
	ctx context.Context, stage Stage, options ...Option,
) ([]byte, error) {
	return cfg.newRunner(stage, options...).output(ctx)
}
