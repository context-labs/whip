package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/localruntime"
	"golang.org/x/sys/unix"
)

// Only disposable tests replace launch/tail. Status and stop always verify the
// native protocol identity instead of consulting a retained PID file.
var launchNativeRuntime = localruntime.Start

var tailDaemonLog = func(file *os.File, lines int, follow bool) error {
	args := []string{"-n", strconv.Itoa(lines)}
	if follow {
		args = append(args, "-f")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/tail", args...)
	command.Stdin, command.Stdout, command.Stderr = file, os.Stdout, os.Stderr
	return command.Run()
}

type nativeDaemonStatus struct {
	localruntime.Status
	ClientBuild string `json:"client_build"`
}

func daemonManageCLI(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: whipcode daemon <status|start|stop|restart|logs>")
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
		return fmt.Errorf("unknown whipcode daemon subcommand %q", args[0])
	}
}

func daemonStatusCLI(args []string) error {
	flags := flag.NewFlagSet("daemon status", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "print native runtime status")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: whipcode daemon status [--json]")
	}
	paths, err := nativeRuntimePaths()
	if err != nil {
		return err
	}
	status := nativeDaemonStatus{Status: localruntime.Inspect(context.Background(), paths), ClientBuild: version}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(status)
	}
	fmt.Printf("state:         %s\n", status.State)
	if process := status.Process; process != nil {
		fmt.Printf("runtime:       %s\nepoch:         %s\npid:           %d\ndaemon build:  %s\nclient build:  %s\nbuild match:   %t\n", process.RuntimeID, process.ProcessEpoch, process.PID, process.Build, version, process.Build == version)
		if started, err := time.Parse(time.RFC3339Nano, process.StartedAt); err == nil {
			fmt.Printf("uptime:        %s\n", time.Since(started).Truncate(time.Second))
		}
		if process.WebState != "" {
			fmt.Printf("network state: %s\n", process.WebState)
		}
		if process.WebError != "" {
			fmt.Printf("network error: %s\n", process.WebError)
		}
		if process.WebEndpoint != "" {
			fmt.Printf("network:       %s\n", process.WebEndpoint)
		}
	}
	fmt.Printf("socket:        %s\ndirectory:     %s\nlog:           %s\n", status.Socket, status.Directory, status.Log)
	if status.Error != "" {
		fmt.Printf("error:         %s\n", status.Error)
	}
	return nil
}

func nativeRuntimeLaunch() (localruntime.Launch, error) {
	executable, err := os.Executable()
	if err != nil {
		return localruntime.Launch{}, err
	}
	launch := localruntime.Launch{Executable: executable, Arguments: []string{"_native-runtime"}, Build: version}
	var network, terminals bool
	for _, setting := range []struct {
		name   string
		target *bool
	}{{"NETWORK", &network}, {"NETWORK_TERMINALS", &terminals}} {
		if value := os.Getenv(buildinfo.Env(setting.name)); value != "" {
			*setting.target, err = strconv.ParseBool(value)
			if err != nil {
				return localruntime.Launch{}, fmt.Errorf("%s must be a boolean", buildinfo.Env(setting.name))
			}
		}
	}
	if network {
		launch.WaitForWeb = true
		launch.Arguments = append(launch.Arguments, "-web")
		for _, setting := range []struct{ name, flag string }{{"LISTEN", "-web-listen"}, {"ALLOWED_HOSTS", "-web-hosts"}, {"ALLOWED_ORIGINS", "-web-origins"}} {
			if value := os.Getenv(buildinfo.Env(setting.name)); value != "" {
				launch.Arguments = append(launch.Arguments, setting.flag, value)
			}
		}
		if terminals {
			launch.Arguments = append(launch.Arguments, "-web-terminals")
		}
	}
	return launch, nil
}

func daemonStartCLI(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: whipcode daemon start (WHIPCODE_NETWORK=1 explicitly enables the native gateway)")
	}
	paths, err := nativeRuntimePaths()
	if err != nil {
		return err
	}
	launch, err := nativeRuntimeLaunch()
	if err != nil {
		return err
	}
	prior := localruntime.Inspect(context.Background(), paths)
	status, err := launchNativeRuntime(context.Background(), paths, launch)
	if err != nil {
		return err
	}
	if status.Process == nil {
		return errors.New("native runtime returned no process identity")
	}
	action := "started"
	if prior.Process != nil && prior.Process.ProcessEpoch == status.Process.ProcessEpoch {
		action = "already running"
	}
	fmt.Printf("daemon %s (pid %d, build %s)\n", action, status.Process.PID, status.Process.Build)
	if status.Process.Build != version {
		fmt.Printf("running build differs from selected build %s; daemon restart explicitly selects the new executable\n", version)
	}
	return nil
}

func daemonLifecycleFlags(name string, args []string) (time.Duration, bool, error) {
	flags := flag.NewFlagSet("daemon "+name, flag.ContinueOnError)
	timeout := flags.Duration("timeout", 10*time.Second, "time to wait for a clean transition (maximum 15s)")
	force := flags.Bool("force", false, "request shutdown; runtime identity checks always apply")
	if err := flags.Parse(args); err != nil {
		return 0, false, err
	}
	if flags.NArg() != 0 || *timeout <= 0 || *timeout > 15*time.Second {
		return 0, false, fmt.Errorf("usage: whipcode daemon %s [--timeout 1ms..15s] [--force]", name)
	}
	return *timeout, *force, nil
}

func stopNativeDaemon(ctx context.Context, paths localruntime.Paths, force bool) (bool, error) {
	running, err := localruntime.Stop(ctx, paths)
	if err != nil && force {
		return running, fmt.Errorf("%w; --force cannot bypass native runtime identity checks or signal an unverified PID", err)
	}
	return running, err
}

func daemonStopCLI(args []string) error {
	timeout, force, err := daemonLifecycleFlags("stop", args)
	if err != nil {
		return err
	}
	paths, err := nativeRuntimePaths()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	running, err := stopNativeDaemon(ctx, paths, force)
	if err != nil {
		return err
	}
	if running {
		fmt.Println("daemon stopped")
	} else {
		fmt.Println("daemon already stopped")
	}
	return nil
}

func daemonRestartCLI(args []string) error {
	timeout, force, err := daemonLifecycleFlags("restart", args)
	if err != nil {
		return err
	}
	paths, err := nativeRuntimePaths()
	if err != nil {
		return err
	}
	launch, err := nativeRuntimeLaunch()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if _, err := stopNativeDaemon(ctx, paths, force); err != nil {
		return err
	}
	status, err := launchNativeRuntime(ctx, paths, launch)
	if err != nil {
		return err
	}
	if status.Process == nil {
		return errors.New("native runtime returned no process identity")
	}
	fmt.Printf("daemon restarted (pid %d, build %s)\n", status.Process.PID, status.Process.Build)
	return nil
}

func daemonLogsCLI(args []string) error {
	flags := flag.NewFlagSet("daemon logs", flag.ContinueOnError)
	follow := flags.Bool("f", false, "follow appended log output")
	flags.Bool("web", false, "gateway output shares the native host log")
	lines := flags.Int("n", 200, "number of lines (maximum 10000)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *lines < 1 || *lines > 10000 {
		return errors.New("usage: whipcode daemon logs [--web] [-f] [-n 1..10000]")
	}
	paths, err := nativeRuntimePaths()
	if err != nil {
		return err
	}
	directoryFD, err := unix.Open(paths.Directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(directoryFD), paths.Directory)
	defer func() { _ = directory.Close() }()
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() || !runRecordOwned(info) || info.Mode().Perm()&0o077 != 0 {
		return errors.New("native runtime directory must be owned and private")
	}
	fd, err := unix.Openat(directoryFD, filepath.Base(paths.Log), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), paths.Log)
	defer func() { _ = file.Close() }()
	info, err = file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !runRecordOwned(info) || info.Mode().Perm()&0o077 != 0 {
		return errors.New("native log must be an owned private regular file")
	}
	return tailDaemonLog(file, *lines, *follow)
}
