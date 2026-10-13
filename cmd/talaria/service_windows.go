//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "Talaria"

// isWindowsService reports whether we were started by the Service Control
// Manager (as opposed to a double-click or a terminal).
func isWindowsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

type talariaService struct {
	run func(stop <-chan struct{})
}

func (s *talariaService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		s.run(stop)
		close(done)
	}()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case c := <-requests:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				close(stop)
				select {
				case <-done:
				case <-time.After(20 * time.Second):
				}
				return false, 0
			}
		case <-done:
			// The server exited on its own (fatal error): report failure so
			// the SCM's recovery actions restart us.
			return false, 1
		}
	}
}

// runWindowsService hands control to the SCM. Services have no console, so
// stdout/stderr go to talaria.log next to the executable.
func runWindowsService(run func(stop <-chan struct{})) {
	if exe, err := os.Executable(); err == nil {
		if f, err := os.OpenFile(filepath.Join(filepath.Dir(exe), "talaria.log"),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			os.Stdout = f
			os.Stderr = f
		}
	}
	if err := svc.Run(serviceName, &talariaService{run: run}); err != nil {
		fmt.Fprintln(os.Stderr, "service run:", err)
		os.Exit(1)
	}
}

// serviceControl implements `talaria -service install|uninstall|start|stop`.
func serviceControl(cmd, configPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as administrator?): %w", err)
	}
	defer m.Disconnect()

	switch cmd {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		cfg, err := filepath.Abs(configPath)
		if err != nil {
			return err
		}
		if s, err := m.OpenService(serviceName); err == nil {
			s.Close()
			return fmt.Errorf("service %q is already installed", serviceName)
		}
		s, err := m.CreateService(serviceName, exe, mgr.Config{
			DisplayName: "Talaria chat server",
			Description: "Self-hosted chat, calls, and listening sessions.",
			StartType:   mgr.StartAutomatic,
		}, "-config", cfg)
		if err != nil {
			return fmt.Errorf("create service: %w", err)
		}
		defer s.Close()
		// Restart on crash: 5s, 10s, then every 30s; counter resets daily.
		_ = s.SetRecoveryActions([]mgr.RecoveryAction{
			{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
			{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
			{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		}, 86400)
		fmt.Printf("Installed service %q\n  binary: %s\n  config: %s\nStart it with: talaria -service start\n", serviceName, exe, cfg)
		return nil

	case "uninstall":
		s, err := m.OpenService(serviceName)
		if err != nil {
			return fmt.Errorf("service %q is not installed", serviceName)
		}
		defer s.Close()
		if st, err := s.Query(); err == nil && st.State != svc.Stopped {
			s.Control(svc.Stop)
			waitForState(s, svc.Stopped, 20*time.Second)
		}
		if err := s.Delete(); err != nil {
			return err
		}
		fmt.Printf("Removed service %q\n", serviceName)
		return nil

	case "start":
		s, err := m.OpenService(serviceName)
		if err != nil {
			return fmt.Errorf("service %q is not installed", serviceName)
		}
		defer s.Close()
		if err := s.Start(); err != nil {
			return err
		}
		fmt.Println("Started", serviceName)
		return nil

	case "stop":
		s, err := m.OpenService(serviceName)
		if err != nil {
			return fmt.Errorf("service %q is not installed", serviceName)
		}
		defer s.Close()
		if _, err := s.Control(svc.Stop); err != nil {
			return err
		}
		waitForState(s, svc.Stopped, 20*time.Second)
		fmt.Println("Stopped", serviceName)
		return nil
	}
	return fmt.Errorf("unknown -service command %q (install, uninstall, start, stop)", cmd)
}

func waitForState(s *mgr.Service, want svc.State, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if st, err := s.Query(); err == nil && st.State == want {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}
