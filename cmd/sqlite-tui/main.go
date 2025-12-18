// sqlite-tui is a TUI and CLI database studio for SQLite.
// It can run locally or as an SSH server.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/johan-st/sqlite-tui/internal/access"
	"github.com/johan-st/sqlite-tui/internal/cli"
	"github.com/johan-st/sqlite-tui/internal/config"
	"github.com/johan-st/sqlite-tui/internal/database"
	"github.com/johan-st/sqlite-tui/internal/history"
	"github.com/johan-st/sqlite-tui/internal/server"
	"github.com/johan-st/sqlite-tui/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	// Parse flags
	sshMode := flag.Bool("ssh", false, "run SSH server mode (requires -config)")
	configPath := flag.String("config", "", "path to config file (required for SSH mode)")
	showVersion := flag.Bool("version", false, "show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("sqlite-tui %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built: %s\n", buildDate)
		os.Exit(0)
	}

	// SSH server mode
	if *sshMode {
		if *configPath == "" {
			log.Fatal("SSH mode requires -config flag")
		}
		if err := runSSHServer(*configPath); err != nil {
			log.Fatalf("SSH server error: %v", err)
		}
		return
	}

	// Local mode - require path argument
	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	pathArg := args[0]
	cmdArgs := args[1:] // Remaining args are command + args

	if len(cmdArgs) > 0 {
		// CLI mode: run command and exit
		if err := runLocalCLI(pathArg, cmdArgs); err != nil {
			log.Fatalf("Error: %v", err)
		}
	} else {
		// TUI mode: interactive
		if err := runLocalTUI(pathArg); err != nil {
			log.Fatalf("TUI error: %v", err)
		}
	}
}

func printUsage() {
	fmt.Println("sqlite-tui - Database Studio for SQLite")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  sqlite-tui <path>                    Interactive TUI mode")
	fmt.Println("  sqlite-tui <path> <command> [args]   CLI mode (run and exit)")
	fmt.Println("  sqlite-tui -ssh -config <file>       SSH server mode")
	fmt.Println()
	fmt.Println("Local mode examples:")
	fmt.Println("  sqlite-tui mydb.db                   Open database in TUI")
	fmt.Println("  sqlite-tui ./databases/              Open all .db files in directory")
	fmt.Println("  sqlite-tui mydb.db ls                List databases")
	fmt.Println("  sqlite-tui mydb.db tables mydb       List tables")
	fmt.Println("  sqlite-tui mydb.db query mydb \"SELECT * FROM users\"")
	fmt.Println()
	fmt.Println("SSH server example:")
	fmt.Println("  sqlite-tui -ssh -config config.yaml")
	fmt.Println()
	fmt.Println("Flags:")
	flag.PrintDefaults()
}

// initLocal creates database manager and user for local mode
func initLocal(pathArg string) (*database.Manager, *access.UserInfo, error) {
	// Create minimal config from path argument
	cfg := config.DefaultConfig()
	cfg.Databases = []config.DatabaseSource{{
		Path:        pathArg,
		Description: "Local database",
	}}

	// Initialize database manager
	dbManager, err := database.NewManager(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize database manager: %w", err)
	}

	if err := dbManager.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start database manager: %w", err)
	}

	// Create local admin user - always admin in local mode
	user := &access.UserInfo{
		Name:    "local",
		IsAdmin: true,
	}

	return dbManager, user, nil
}

// runLocalCLI runs a CLI command in local mode
func runLocalCLI(pathArg string, cmdArgs []string) error {
	dbManager, user, err := initLocal(pathArg)
	if err != nil {
		return err
	}
	defer dbManager.Stop()

	// Create CLI handler (no history store in local mode)
	handler := cli.NewHandler(dbManager, nil, version)

	// Execute command using local context
	ctx := cli.NewLocalContext(user, cmdArgs, os.Stdout, os.Stderr)
	return handler.HandleLocal(ctx)
}

// runLocalTUI runs the interactive TUI in local mode
func runLocalTUI(pathArg string) error {
	dbManager, user, err := initLocal(pathArg)
	if err != nil {
		return err
	}
	defer dbManager.Stop()

	// Get terminal size
	width, height := 80, 24
	fd := int(os.Stdout.Fd())
	if term.IsTerminal(fd) {
		if w, h, err := term.GetSize(fd); err == nil {
			width, height = w, h
		}
	}

	// Create and run TUI
	app := tui.NewApp(dbManager, nil, user, width, height)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

// runSSHServer runs the SSH server mode
func runSSHServer(configPath string) error {
	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize history store
	historyStore, err := history.NewStore(cfg.GetDataDir())
	if err != nil {
		return fmt.Errorf("failed to initialize history store: %w", err)
	}
	defer historyStore.Close()

	// Initialize database manager
	dbManager, err := database.NewManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize database manager: %w", err)
	}

	if err := dbManager.Start(); err != nil {
		return fmt.Errorf("failed to start database manager: %w", err)
	}
	defer dbManager.Stop()

	// Start config watcher for hot-reloading
	configWatcher, err := config.NewWatcher(cfg)
	if err != nil {
		log.Printf("Warning: Failed to create config watcher: %v", err)
	} else {
		configWatcher.OnReload(func(newCfg *config.Config) {
			log.Println("Config reloaded, updating resolver...")
			dbManager.UpdateResolver(newCfg.BuildResolver())
			dbManager.GetDiscovery().UpdateSources(newCfg.Databases)
		})
		if err := configWatcher.Start(); err != nil {
			log.Printf("Warning: Failed to start config watcher: %v", err)
		} else {
			defer configWatcher.Stop()
		}
	}

	// Create CLI handler
	cliHandler := cli.NewHandler(dbManager, historyStore, version)

	// Create and configure SSH server
	sshServer := server.NewServer(cfg, dbManager, historyStore)
	sshServer.SetCLIHandler(cliHandler.Handle)
	sshServer.SetTUIHandler(tui.Handler(dbManager, historyStore))

	log.Printf("Starting SSH server on %s", cfg.Server.SSH.Listen)
	return sshServer.Start()
}
