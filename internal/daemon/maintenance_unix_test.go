//go:build unix

package daemon

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestMaintenanceExcludesStartsAndOtherUpdaters(t *testing.T) {
	paths, err := Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	maintenance, err := AcquireMaintenance(paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = maintenance.Close() })
	if lock, err := AcquireStartup(paths, 0); !errors.Is(err, ErrMaintenance) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("startup entered maintenance: %v", err)
	}
	if lock, err := AcquireMaintenance(paths); !errors.Is(err, ErrMaintenance) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("second updater entered maintenance: %v", err)
	}
	if err := maintenance.Close(); err != nil {
		t.Fatal(err)
	}
	startup, err := AcquireStartup(paths, 0)
	if err != nil {
		t.Fatal(err)
	}
	if lock, err := AcquireMaintenance(paths); !errors.Is(err, ErrMaintenance) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("updater raced startup ownership: %v", err)
	}
	_ = startup.Close()
	maintenance, err = AcquireMaintenance(paths)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceInheritedDescriptor(t *testing.T) {
	if home := os.Getenv("WHIP_MAINTENANCE_CHILD"); home != "" {
		paths, err := ResolvePaths(home)
		if err != nil {
			t.Fatal(err)
		}
		startup, err := AcquireStartup(paths, 3)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := AcquireOwner(paths.Lock)
		if err != nil {
			t.Fatal(err)
		}
		_ = startup.Close()
		_ = owner.Close()
		return
	}
	home := t.TempDir()
	paths, err := Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	maintenance, err := AcquireMaintenance(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = maintenance.Close() }()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMaintenanceInheritedDescriptor$")
	command.Env = append(os.Environ(), "WHIP_MAINTENANCE_CHILD="+home)
	command.ExtraFiles = []*os.File{maintenance}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("child: %s, %v", output, err)
	}
	if lock, err := AcquireMaintenance(paths); !errors.Is(err, ErrMaintenance) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("child close unlocked parent: %v", err)
	}
}

func TestMaintenanceRefusesSymlinkAndForeignDescriptor(t *testing.T) {
	paths, err := Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if lock, err := AcquireStartup(paths, 99); err == nil {
		_ = lock.Close()
		t.Fatal("accepted unrelated descriptor")
	}
	name := filepath.Join(paths.Runtime, "maintenance.lock")
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(paths.Runtime, "elsewhere"), name); err != nil {
		t.Fatal(err)
	}
	if lock, err := AcquireMaintenance(paths); err == nil {
		_ = lock.Close()
		t.Fatal("accepted symlink lock")
	}
}

func TestMaintenanceRefusesUnsafeExistingLock(t *testing.T) {
	for _, kind := range []string{"shared", "directory", "missing-parent"} {
		t.Run(kind, func(t *testing.T) {
			paths, err := Paths(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(paths.Runtime, "maintenance.lock")
			switch kind {
			case "shared":
				if err := os.WriteFile(name, nil, 0o644); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(name, 0o700); err != nil {
					t.Fatal(err)
				}
			case "missing-parent":
				paths.Runtime = filepath.Join(paths.Runtime, "missing")
			}
			if file, err := AcquireMaintenance(paths); err == nil {
				file.Close()
				t.Fatal("unsafe lock accepted for update")
			}
			if file, err := AcquireStartup(paths, 0); err == nil {
				file.Close()
				t.Fatal("unsafe lock accepted for startup")
			}
		})
	}
}

func TestMaintenanceLaunchFailuresReleaseResources(t *testing.T) {
	paths, err := Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	maintenance, err := AcquireMaintenance(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer maintenance.Close()
	if err := LaunchInstalledDaemon(paths, filepath.Join(paths.Home, "missing-executable"), maintenance); err == nil {
		t.Fatal("missing executable started")
	}
	paths.Home = filepath.Join(paths.Home, "missing-parent")
	if err := LaunchInstalledDaemon(paths, "/bin/false", maintenance); err == nil {
		t.Fatal("missing log directory accepted")
	}
}

func TestMaintenanceValidatesInheritedOpenFileDescription(t *testing.T) {
	for _, kind := range []string{"shared-description", "wrong-file", "closed", "unlocked", "independent-description"} {
		t.Run(kind, func(t *testing.T) {
			paths, err := Paths(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			original, err := maintenanceFile(paths)
			if err != nil {
				t.Fatal(err)
			}
			defer original.Close()
			if kind != "unlocked" {
				if err := unix.Flock(int(original.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			probe, err := maintenanceFile(paths)
			if err != nil {
				t.Fatal(err)
			}
			defer probe.Close()
			fd, err := unix.Dup(int(original.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			inherited := os.NewFile(uintptr(fd), "inherited-fixture")
			defer func() { inherited.Close() }()
			switch kind {
			case "closed":
				inherited.Close()
			case "wrong-file", "independent-description":
				inherited.Close()
				if kind == "wrong-file" {
					inherited, err = os.CreateTemp(t.TempDir(), "wrong-lock")
				} else {
					inherited, err = maintenanceFile(paths)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			err = validateInheritedMaintenance(probe, inherited)
			if kind != "shared-description" {
				if err == nil {
					t.Fatal("invalid inherited ownership accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := inherited.Close(); err != nil {
				t.Fatal(err)
			}
			if lock, err := AcquireMaintenance(paths); !errors.Is(err, ErrMaintenance) {
				if lock != nil {
					lock.Close()
				}
				t.Fatalf("closing child released the parent fence: %v", err)
			}
		})
	}
}
