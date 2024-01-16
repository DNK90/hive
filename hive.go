package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"

	"github.com/ethereum/hive/internal/libdocker"
	"github.com/ethereum/hive/internal/libhive"
	"github.com/spf13/viper"
	"gopkg.in/inconshreveable/log15.v2"
)

type Config struct {
	Clients       string            `yaml:"clients" mapstructure:"clients"`
	ClientsFile   string            `yaml:"clientsFile" mapstructure:"clientsFile"`
	ClientTimeout time.Duration     `yaml:"clientTimeout" mapstructure:"clientTimeout"`
	resultsRoot   string            `yaml:"resultsRoot" mapstructure:"resultsRoot"`
	LogLevel      int               `yaml:"loglevel" mapstructure:"loglevel"`
	Docker        *Docker           `yaml:"docker" mapstructure:"docker"`
	Sim           *Simulator        `yaml:"sim" mapstructure:"sim"`
	Mounts        []string          `yaml:"mounts" mapstructure:"mounts"`
	Envs          map[string]string `yaml:"envs" mapstructure:"envs"`
}

type Docker struct {
	Endpoint      string `yaml:"endpoint" mapstructure:"endpoint"`
	Nocache       string `yaml:"nocache" mapstructure:"nocache"`
	Pull          bool   `yaml:"pull" mapstructure:"pull"`
	Output        bool   `yaml:"output" mapstructure:"output"`
	UseCredHelper bool   `yaml:"credHelper" mapstructure:"credHelper"`
}

type Simulator struct {
	Pattern            string        `yaml:"pattern" mapstructure:"pattern"`
	TestPattern        string        `yaml:"limit" mapstructure:"limit"`
	Parallelism        int           `yaml:"parallelism" mapstructure:"parallelism"`
	RandomSeed         int           `yaml:"randomSeed" mapstructure:"randomSeed"`
	TimeLimit          time.Duration `yaml:"timeLimit" mapstructure:"timeLimit"`
	Loglevel           int           `yaml:"loglevel" mapstructure:"loglevel"`
	DevMode            bool          `yaml:"dev" mapstructure:"dev"`
	DevModeAPIEndpoint string        `yaml:"devAddr" mapstructure:"devAddr"`
}

var cfg = &Config{
	resultsRoot: "workspace/logs",
	LogLevel:    3,
	Sim: &Simulator{
		Parallelism:        1,
		RandomSeed:         0,
		TimeLimit:          0,
		Loglevel:           3,
		DevMode:            false,
		DevModeAPIEndpoint: "127.0.0.1:3000",
	},
	Docker:        &Docker{},
	ClientTimeout: 3 * time.Minute,
	Mounts:        make([]string, 0),
	Envs:          make(map[string]string),
}

func Load(path string, cfg interface{}) {
	fmt.Println(fmt.Sprintf("loading config from file : %s", path))
	plan, err := ioutil.ReadFile(path)
	if err != nil {
		if err.Error() == "path is empty" {
			return
		}
		panic(err)
	}
	UnmarshalConfig(plan, cfg)
}

func UnmarshalConfig(data []byte, cfg interface{}) {
	viper.SetConfigType("yaml")
	err := viper.ReadConfig(bytes.NewBuffer(data))
	if err != nil {
		panic(err)
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "__"))
	viper.AutomaticEnv()
	err = viper.Unmarshal(&cfg)
	if err != nil {
		panic(err)
	}
}

func loadConfig() {
	// load config from env first
	if os.Getenv("CONFIG") != "" {
		Load(os.Getenv("CONFIG"), cfg)
	}
	// parse arguments and add to config
	flag.StringVar(&cfg.resultsRoot, "results-root", cfg.resultsRoot, "Target `directory` for results files and logs.")
	flag.IntVar(&cfg.LogLevel, "loglevel", cfg.LogLevel, "Log `level` for system events. Supports values 0-5.")
	flag.StringVar(&cfg.Docker.Endpoint, "docker.endpoint", cfg.Docker.Endpoint, "Endpoint of the local Docker daemon.")
	flag.StringVar(&cfg.Docker.Nocache, "docker.nocache", cfg.Docker.Nocache, "Regular `expression` selecting the docker images to forcibly rebuild.")
	flag.BoolVar(&cfg.Docker.Pull, "docker.pull", cfg.Docker.Pull, "Refresh base images when building images.")
	flag.BoolVar(&cfg.Docker.Output, "docker.output", cfg.Docker.Output, "Relay all docker output to stderr.")
	flag.StringVar(&cfg.Sim.Pattern, "sim", cfg.Sim.Pattern, "Regular `expression` selecting the simulators to run.")
	flag.StringVar(&cfg.Sim.TestPattern, "sim.limit", cfg.Sim.TestPattern, "Regular `expression` selecting tests/suites (interpreted by simulators).")
	flag.IntVar(&cfg.Sim.Parallelism, "sim.parallelism", cfg.Sim.Parallelism, "Max `number` of parallel clients/containers (interpreted by simulators).")
	flag.IntVar(&cfg.Sim.RandomSeed, "sim.randomseed", cfg.Sim.RandomSeed, "Randomness seed number (interpreted by simulators).")
	flag.DurationVar(&cfg.Sim.TimeLimit, "sim.timelimit", cfg.Sim.TimeLimit, "Simulation `timeout`. Hive aborts the simulator if it exceeds this time.")
	flag.IntVar(&cfg.Sim.Loglevel, "sim.loglevel", cfg.Sim.Loglevel, "Selects log `level` of client instances. Supports values 0-5.")
	flag.BoolVar(&cfg.Sim.DevMode, "dev", cfg.Sim.DevMode, "Only starts the simulator API endpoint (listening at 127.0.0.1:3000 by default) without starting any simulators.")
	flag.StringVar(&cfg.Sim.DevModeAPIEndpoint, "dev.addr", cfg.Sim.DevModeAPIEndpoint, "Endpoint that the simulator API listens on")
	flag.BoolVar(&cfg.Docker.UseCredHelper, "docker.cred-helper", cfg.Docker.UseCredHelper, "configure docker authentication using locally-configured credential helper")

	flag.StringVar(&cfg.ClientsFile, "client-file", cfg.ClientsFile, `YAML `+"`file`"+` containing client configurations.`)

	flag.StringVar(&cfg.Clients, "client", cfg.Clients, "Comma separated `list` of clients to use. Client names in the list may be given as\n"+
		"just the client name, or a client_branch specifier. If a branch name is supplied,\n"+
		"the client image will use the given git branch or docker tag. Multiple instances of\n"+
		"a single client type may be requested with different branches.\n"+
		"Example: \"besu_latest,besu_20.10.2\"\n")

	flag.DurationVar(&cfg.ClientTimeout, "client.checktimelimit", cfg.ClientTimeout, "The `timeout` of waiting for clients to open up the RPC port.\n"+
		"If a very long chain is imported, this timeout may need to be quite large.\n"+
		"A lower value means that hive won't wait as long in case the node crashes and\n"+
		"never opens the RPC port.")

	// Parse the flags and configure the logger.
	flag.Parse()
}

func main() {
	loadConfig()
	log15.Root().SetHandler(log15.LvlFilterHandler(log15.Lvl(cfg.LogLevel), log15.StreamHandler(os.Stderr, log15.TerminalFormat())))

	// Get the list of simulators.
	inv, err := libhive.LoadInventory(".")
	if err != nil {
		fatal(err)
	}
	simList, err := inv.MatchSimulators(cfg.Sim.Pattern)
	if err != nil {
		fatal("bad --sim regular expression:", err)
	}
	if cfg.Sim.Pattern != "" && len(simList) == 0 {
		fatal("no simulators for pattern", cfg.Sim.Pattern)
	}
	if cfg.Sim.Pattern != "" && cfg.Sim.DevMode {
		log15.Warn("--sim is ignored when using --dev mode")
		simList = nil
	}

	// Create the docker backends.
	dockerConfig := &libdocker.Config{
		Inventory:           inv,
		PullEnabled:         cfg.Docker.Pull,
		UseCredentialHelper: cfg.Docker.UseCredHelper,
	}
	if cfg.Docker.Nocache != "" {
		re, err := regexp.Compile(cfg.Docker.Nocache)
		if err != nil {
			fatal("bad --docker-nocache regular expression:", err)
		}
		dockerConfig.NoCachePattern = re
	}
	if cfg.Docker.Output {
		dockerConfig.ContainerOutput = os.Stderr
		dockerConfig.BuildOutput = os.Stderr
	}
	builder, cb, err := libdocker.Connect(cfg.Docker.Endpoint, dockerConfig)
	if err != nil {
		fatal(err)
	}

	// Set up the context for CLI interrupts.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-sig
		cancel()
	}()

	// Run.
	env := libhive.SimEnv{
		LogDir:             cfg.resultsRoot,
		SimLogLevel:        cfg.Sim.Loglevel,
		SimTestPattern:     cfg.Sim.TestPattern,
		SimParallelism:     cfg.Sim.Parallelism,
		SimRandomSeed:      cfg.Sim.RandomSeed,
		SimDurationLimit:   cfg.Sim.TimeLimit,
		ClientStartTimeout: cfg.ClientTimeout,
		Mounts:             cfg.Mounts,
		Envs:               cfg.Envs,
	}
	runner := libhive.NewRunner(inv, builder, cb)

	// Parse the client list.
	// It can be supplied as a comma-separated list, or as a YAML file.
	var clientList []libhive.ClientDesignator
	if cfg.ClientsFile == "" {
		clientList, err = libhive.ParseClientList(&inv, cfg.Clients)
		if err != nil {
			fatal("-client:", err)
		}
	} else {
		clientList, err = parseClientsFile(&inv, cfg.ClientsFile)
		if err != nil {
			fatal("-client-file:", err)
		}
		// If YAML file is used, the list can be filtered by the -client flag.
		if cfg.Clients != "" {
			filter := strings.Split(cfg.Clients, ",")
			clientList = libhive.FilterClients(clientList, filter)
		}
	}

	// Build clients and simulators.
	if err := runner.Build(ctx, clientList, simList); err != nil {
		fatal(err)
	}
	if cfg.Sim.DevMode {
		runner.RunDevMode(ctx, env, cfg.Sim.DevModeAPIEndpoint)
		return
	}

	// Run simulators.
	var failCount int
	for _, sim := range simList {
		result, err := runner.Run(ctx, sim, env)
		if err != nil {
			fatal(err)
		}
		failCount += result.TestsFailed
		log15.Info(fmt.Sprintf("simulation %s finished", sim), "suites", result.Suites, "tests", result.Tests, "failed", result.TestsFailed)
	}

	switch failCount {
	case 0:
	case 1:
		fatal(errors.New("1 test failed"))
	default:
		fatal(fmt.Errorf("%d tests failed", failCount))
	}
}

func fatal(args ...interface{}) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}

func parseClientsFile(inv *libhive.Inventory, file string) ([]libhive.ClientDesignator, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return libhive.ParseClientListYAML(inv, f)
}

func flagIsSet(name string) bool {
	var found bool
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
