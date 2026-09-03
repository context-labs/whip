package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/session"
)

const daemonManageTimeout = 10 * time.Second

var (
	launchManagedDaemon = daemon.LaunchSelfDaemon
	findDaemonProcess   = os.FindProcess
	tailDaemonLog       = func(path string, lines int, follow bool) error {
		args := []string{"-n", strconv.Itoa(lines)}
		if follow {
			args = append(args, "-f")
		}
		command := exec.Command("tail", append(args, path)...)
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		return command.Run()
	}
)

type daemonStatus struct {
	State         string `json:"state"`
	PID           int    `json:"pid,omitempty"`
	Generation    int64  `json:"generation,omitempty"`
	DaemonBuild   string `json:"daemon_build,omitempty"`
	ClientBuild   string `json:"client_build"`
	BuildMatch    bool   `json:"build_match"`
	StartedAt     string `json:"started_at,omitempty"`
	UptimeSeconds int64  `json:"uptime_seconds,omitempty"`
	Socket        string `json:"socket"`
	Database      string `json:"database"`
	Log           string `json:"log"`
	Error         string `json:"error,omitempty"`
}

func daemonManageCLI(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: whip daemon <status|start|stop|restart|logs>")
	}
	switch args[0] {
	case "status":
		return daemonStatusCLI(args[1:])
	case "start":
		return daemonStartCLI(args[1:])
	case "stop":
		return daemonStopCLI(args[1:])
	case "restart":
		return daemonRestartCLI(args[1:])
	case "logs":
		return daemonLogsCLI(args[1:])
	default:
		return fmt.Errorf("unknown whip daemon subcommand %q (want: status, start, stop, restart, or logs)", args[0])
	}
}

func daemonRuntimePaths() (daemon.RuntimePaths, error) {
	dir, err := config.Dir()
	if err != nil {
		return daemon.RuntimePaths{}, err
	}
	return daemon.Paths(dir)
}

func daemonStatusCLI(args []string) error {
	flags := flag.NewFlagSet("whip daemon status", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "print machine-readable status")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: whip daemon status [--json]")
	}
	paths, err := daemonRuntimePaths()
	if err != nil {
		return err
	}
	status, client := probeDaemon(paths, time.Second)
	if client != nil {
		_ = client.Close()
	}
	if *jsonOutput {
		encoded, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	printDaemonStatus(status)
	return nil
}

func daemonStartCLI(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: whip daemon start")
	}
	paths, err := daemonRuntimePaths()
	if err != nil {
		return err
	}
	status, client := probeDaemon(paths, time.Second)
	if client != nil {
		_ = client.Close()
	}
	if status.State == "running" {
		fmt.Printf("daemon already running (pid %s, build %s)\n", printablePID(status.PID), status.DaemonBuild)
		if !status.BuildMatch {
			fmt.Printf("warning: current CLI build is %s; run `whip daemon restart` to replace the daemon\n", version)
		}
		return nil
	}
	started, err := startManagedDaemon(paths, daemonManageTimeout)
	if err != nil {
		return err
	}
	fmt.Printf("daemon started (pid %s, build %s)\n", printablePID(started.PID), started.DaemonBuild)
	return nil
}

func daemonStopCLI(args []string) error {
	timeout, force, err := daemonLifecycleFlags("stop", args)
	if err != nil {
		return err
	}
	paths, err := daemonRuntimePaths()
	if err != nil {
		return err
	}
	wasRunning, err := stopManagedDaemon(paths, timeout, force)
	if err != nil {
		return err
	}
	if !wasRunning {
		fmt.Println("daemon already stopped")
		return nil
	}
	fmt.Println("daemon stopped")
	return nil
}

func daemonRestartCLI(args []string) error {
	timeout, force, err := daemonLifecycleFlags("restart", args)
	if err != nil {
		return err
	}
	paths, err := daemonRuntimePaths()
	if err != nil {
		return err
	}
	if _, err := stopManagedDaemon(paths, timeout, force); err != nil {
		return err
	}
	started, err := startManagedDaemon(paths, timeout)
	if err != nil {
		return err
	}
	fmt.Printf("daemon restarted (pid %s, build %s)\n", printablePID(started.PID), started.DaemonBuild)
	return nil
}

func daemonLifecycleFlags(name string, args []string) (time.Duration, bool, error) {
	flags := flag.NewFlagSet("whip daemon "+name, flag.ContinueOnError)
	timeout := flags.Duration("timeout", daemonManageTimeout, "time to wait for a clean lifecycle transition")
	force := flags.Bool("force", false, "terminate the recorded daemon process if graceful shutdown fails")
	if err := flags.Parse(args); err != nil {
		return 0, false, err
	}
	if flags.NArg() != 0 || *timeout <= 0 {
		return 0, false, fmt.Errorf("usage: whip daemon %s [--timeout 10s] [--force]", name)
	}
	return *timeout, *force, nil
}

func daemonLogsCLI(args []string) error {
	flags := flag.NewFlagSet("whip daemon logs", flag.ContinueOnError)
	follow := flags.Bool("f", false, "follow appended log output")
	lines := flags.Int("n", 200, "number of lines to print")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *lines <= 0 {
		return errors.New("usage: whip daemon logs [-f] [-n 200]")
	}
	paths, err := daemonRuntimePaths()
	if err != nil {
		return err
	}
	path := filepath.Join(paths.Home, "daemon.log")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("daemon log does not exist yet: %s", path)
		}
		return err
	}
	return tailDaemonLog(path, *lines, *follow)
}

func probeDaemon(paths daemon.RuntimePaths, timeout time.Duration) (daemonStatus, *daemon.Client) {
	status := daemonStatus{
		State: "stopped", ClientBuild: version, Socket: paths.Socket,
		Database: filepath.Join(paths.Home, "sessions.db"), Log: filepath.Join(paths.Home, "daemon.log"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	client, err := daemon.DialClient(ctx, paths, daemon.InitializeParams{
		ProtocolMajor: daemon.ProtocolMajor, BuildID: version,
		ClientID: daemonClientID("daemon-status"), ClientKind: "automation",
	})
	if err != nil {
		pid, owned, ownerErr := daemon.ActiveOwnerPID(paths.Lock)
		if owned || ownerErr != nil {
			status.State, status.PID, status.Error = "unhealthy", pid, err.Error()
			if ownerErr != nil {
				status.Error = errors.Join(err, ownerErr).Error()
			}
		} else if _, statErr := os.Lstat(paths.Socket); statErr == nil {
			status.State, status.Error = "unhealthy", err.Error()
		}
		return status, nil
	}
	initialized := client.InitializeResult()
	status.State = "running"
	status.PID = initialized.PID
	status.Generation = initialized.Generation
	status.DaemonBuild = initialized.BuildID
	status.BuildMatch = initialized.BuildID == version
	status.StartedAt = initialized.StartedAt
	if started, parseErr := time.Parse(time.RFC3339Nano, initialized.StartedAt); parseErr == nil {
		status.UptimeSeconds = max(0, int64(time.Since(started).Seconds()))
	}
	return status, client
}

func startManagedDaemon(paths daemon.RuntimePaths, timeout time.Duration) (daemonStatus, error) {
	if err := launchManagedDaemon(paths); err != nil && !errors.Is(err, daemon.ErrDaemonOwned) {
		return daemonStatus{}, err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, client := probeDaemon(paths, min(250*time.Millisecond, time.Until(deadline)))
		if client != nil {
			_ = client.Close()
			if !status.BuildMatch {
				return daemonStatus{}, fmt.Errorf("daemon started with build %q instead of current build %q", status.DaemonBuild, version)
			}
			return status, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return daemonStatus{}, fmt.Errorf("daemon did not become ready within %s; inspect %s", timeout, filepath.Join(paths.Home, "daemon.log"))
}

func stopManagedDaemon(paths daemon.RuntimePaths, timeout time.Duration, force bool) (bool, error) {
	deadline := time.Now().Add(timeout)
	status, client := probeDaemon(paths, min(time.Second, timeout))
	if client == nil {
		if status.State == "stopped" {
			return false, nil
		}
		if !force {
			return false, fmt.Errorf("daemon is unhealthy: %s (retry with --force)", status.Error)
		}
		return true, forceStopManagedDaemon(paths, timeout)
	}
	defer func() { _ = client.Close() }()
	payload, _ := json.Marshal(map[string]string{"reason": "daemon command"})
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := client.Command(ctx, daemon.CommandParams{
		CommandID: daemonCommandID(daemonClientID("daemon-stop"), "checkpoint"),
		Scope:     string(session.CommandScopeDaemon), Operation: "daemon.checkpoint", Payload: payload,
	})
	if err == nil && result.Status != "succeeded" {
		err = errors.New(result.Error)
		if result.Error == "" {
			err = fmt.Errorf("daemon checkpoint is %s", result.Status)
		}
	}
	var notice daemon.RestartNotice
	if err == nil {
		err = json.Unmarshal([]byte(result.Output), &notice)
	}
	if err == nil {
		err = client.RequestStop(ctx, notice.Generation)
	}
	if err == nil {
		err = waitForDaemonStop(paths, time.Until(deadline))
	}
	if err == nil {
		return true, nil
	}
	if !force {
		return true, err
	}
	return true, forceStopManagedDaemon(paths, timeout)
}

func waitForDaemonStop(paths daemon.RuntimePaths, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, owned, err := daemon.ActiveOwnerPID(paths.Lock)
		if err == nil && !owned {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("daemon did not stop within %s", timeout)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func forceStopManagedDaemon(paths daemon.RuntimePaths, timeout time.Duration) error {
	pid, owned, err := daemon.ActiveOwnerPID(paths.Lock)
	if err != nil {
		return fmt.Errorf("identify daemon owner: %w", err)
	}
	if !owned {
		return nil
	}
	if pid == os.Getpid() {
		return errors.New("refusing to signal the current process")
	}
	process, err := findDaemonProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	grace := min(timeout/2, 2*time.Second)
	if err := waitForDaemonStop(paths, grace); err == nil {
		return nil
	}
	if err := process.Kill(); err != nil {
		return err
	}
	return waitForDaemonStop(paths, max(time.Millisecond, timeout-grace))
}

func printDaemonStatus(status daemonStatus) {
	fmt.Printf("state:         %s\n", status.State)
	if status.State == "running" {
		fmt.Printf("pid:           %s\n", printablePID(status.PID))
		fmt.Printf("generation:    %d\n", status.Generation)
		fmt.Printf("daemon build:  %s\n", status.DaemonBuild)
		fmt.Printf("client build:  %s\n", status.ClientBuild)
		fmt.Printf("build match:   %t\n", status.BuildMatch)
		if status.StartedAt != "" {
			fmt.Printf("uptime:        %s\n", (time.Duration(status.UptimeSeconds) * time.Second).String())
		}
	} else if status.PID > 0 {
		fmt.Printf("pid:           %d\n", status.PID)
	}
	fmt.Printf("socket:        %s\n", status.Socket)
	fmt.Printf("database:      %s\n", status.Database)
	fmt.Printf("log:           %s\n", status.Log)
	if status.Error != "" {
		fmt.Printf("error:         %s\n", status.Error)
	}
}

func printablePID(pid int) string {
	if pid <= 0 {
		return "unknown"
	}
	return strconv.Itoa(pid)
}
