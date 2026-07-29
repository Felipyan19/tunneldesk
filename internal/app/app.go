package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Felipyan19/tunneldesk/internal/autostart"
	"github.com/Felipyan19/tunneldesk/internal/openvpn"
	"github.com/Felipyan19/tunneldesk/internal/profile"
	"github.com/Felipyan19/tunneldesk/internal/secretinput"
	"github.com/Felipyan19/tunneldesk/internal/vault"
)

type Application struct {
	Profiles *profile.Store
	Vault    vault.Vault
	VPN      *openvpn.Manager
	In       io.Reader
	Out      io.Writer
}

func New() (*Application, error) {
	root, err := profile.DefaultRoot()
	if err != nil {
		return nil, err
	}
	store := profile.NewStore(root)
	secureVault := vault.New(root)
	return &Application{
		Profiles: store,
		Vault:    secureVault,
		VPN:      &openvpn.Manager{Profiles: store, Vault: secureVault, Binary: openvpn.FindBinary()},
		In:       os.Stdin,
		Out:      os.Stdout,
	}, nil
}

func Run(args []string) error {
	application, err := New()
	if err != nil {
		return err
	}
	return application.Run(args)
}

func (a *Application) Run(args []string) error {
	if len(args) == 0 || args[0] == "ui" {
		return a.ServeUI()
	}
	switch args[0] {
	case "profile":
		return a.profileCommand(args[1:])
	case "profiles":
		return a.listProfiles()
	case "connect", "disconnect", "status", "logs":
		name, err := profileArgument(args[1:])
		if err != nil {
			return err
		}
		return a.connectionCommand(args[0], name)
	case "run":
		if len(args) != 3 {
			return errors.New("invalid internal runner invocation")
		}
		name, err := profileArgument(args[1:2])
		if err != nil {
			return err
		}
		return a.runProfile(name, args[2])
	case "credentials":
		return a.credentialsCommand(args[1:])
	case "autostart":
		return a.autostartCommand(args[1:])
	case "autoconnect":
		name, err := profileArgument(args[1:])
		if err != nil {
			return err
		}
		return a.autoConnect(name)
	case "help", "--help", "-h":
		fmt.Fprint(a.Out, help)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], help)
	}
}

func profileArgument(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("specify one profile, for example --work-vpn")
	}
	value := strings.TrimSpace(args[0])
	if strings.HasPrefix(value, "--") && len(value) > 2 {
		return strings.TrimPrefix(value, "--"), nil
	}
	if value != "" && !strings.HasPrefix(value, "-") {
		return value, nil
	}
	return "", errors.New("invalid profile; use --profile-name")
}

func (a *Application) profileCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("use profile add or profile remove")
	}
	switch args[0] {
	case "add":
		name, config, err := parseProfileAdd(args[1:])
		if err != nil {
			return err
		}
		p, err := a.Profiles.Add(name, config)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "Profile %q imported.\n", p.Name)
		return nil
	case "remove":
		if len(args) != 2 {
			return errors.New("usage: tunneldesk profile remove <name>")
		}
		p, err := a.Profiles.Get(args[1])
		if err != nil {
			return err
		}
		state, err := a.VPN.Status(p.Name)
		if err != nil {
			return err
		}
		if state.Status == openvpn.StatusConnecting || state.Status == openvpn.StatusConnected {
			return fmt.Errorf("profile %q is connected; disconnect it before removing it", p.Name)
		}
		if p.AutoConnect {
			if err := autostart.Disable(p.Name); err != nil {
				return fmt.Errorf("disable autostart before removing profile: %w", err)
			}
		}
		if err := a.Vault.Delete(p.Name); err != nil {
			return err
		}
		if err := a.Profiles.Remove(p.Name); err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "Profile %q removed.\n", p.Name)
		return nil
	default:
		return fmt.Errorf("unknown profile command %q", args[0])
	}
}

func parseProfileAdd(args []string) (string, string, error) {
	var name, config string
	for index := 0; index < len(args); index++ {
		value := args[index]
		switch {
		case value == "--config" && index+1 < len(args):
			index++
			config = args[index]
		case strings.HasPrefix(value, "--config="):
			config = strings.TrimPrefix(value, "--config=")
		case !strings.HasPrefix(value, "-") && name == "":
			name = value
		default:
			return "", "", errors.New(`usage: tunneldesk profile add <name> --config "C:\path\client.ovpn"`)
		}
	}
	if name == "" || config == "" {
		return "", "", errors.New(`usage: tunneldesk profile add <name> --config "C:\path\client.ovpn"`)
	}
	return name, config, nil
}

func (a *Application) listProfiles() error {
	profiles, err := a.Profiles.List()
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		fmt.Fprintln(a.Out, "No profiles configured.")
		return nil
	}
	for _, p := range profiles {
		state, _ := a.VPN.Status(p.Name)
		fmt.Fprintf(a.Out, "%-20s %s\n", p.Name, state.Status)
	}
	return nil
}

func (a *Application) connectionCommand(command, name string) error {
	switch command {
	case "connect":
		state, err := a.startRunner(name)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "Connected %q (PID %d).\n", state.Profile, state.PID)
	case "disconnect":
		if err := a.VPN.Disconnect(name); err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "Disconnected %q.\n", name)
	case "status":
		state, err := a.VPN.Status(name)
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(state, "", "  ")
		fmt.Fprintln(a.Out, string(data))
	case "logs":
		logs, err := a.VPN.Logs(name, 100)
		if err != nil {
			return err
		}
		fmt.Fprintln(a.Out, logs)
	}
	return nil
}

func (a *Application) startRunner(name string) (openvpn.State, error) {
	p, err := a.Profiles.Get(name)
	if err != nil {
		return openvpn.State{}, err
	}
	name = p.Name
	state, _ := a.VPN.Status(name)
	if state.Status == openvpn.StatusConnected || state.Status == openvpn.StatusConnecting {
		return openvpn.State{}, fmt.Errorf("profile %q is already running", name)
	}
	executable, err := os.Executable()
	if err != nil {
		return openvpn.State{}, err
	}
	sessionID, err := openvpn.NewSessionID()
	if err != nil {
		return openvpn.State{}, err
	}
	cmd := exec.Command(executable, "run", "--"+name, sessionID)
	hideRunnerWindow(cmd)
	if err := cmd.Start(); err != nil {
		return openvpn.State{}, fmt.Errorf("start TunnelDesk background runner: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return openvpn.State{}, err
	}
	return a.VPN.WaitForResult(name, sessionID, 30*time.Second)
}

func (a *Application) runProfile(name, sessionID string) error {
	p, err := a.Profiles.Get(name)
	if err != nil {
		return err
	}
	name = p.Name
	if err := a.VPN.Connect(name, sessionID); err != nil {
		a.VPN.RecordFailure(name, sessionID, err)
		return err
	}
	wasConnected := false
	retryDelay := time.Second
	for {
		state, err := a.VPN.Status(name)
		if err != nil {
			return err
		}
		if state.SessionID != sessionID {
			return nil
		}
		if state.Status == openvpn.StatusConnected {
			wasConnected = true
		}
		if state.Status == openvpn.StatusDisconnected || state.Status == openvpn.StatusFailed {
			if state.StopRequested || !p.Reconnect || !wasConnected {
				if state.Status == openvpn.StatusFailed && state.Error != "" {
					return errors.New(state.Error)
				}
				return nil
			}
			time.Sleep(retryDelay)
			if retryDelay < 30*time.Second {
				retryDelay *= 2
			}
			nextSessionID, err := openvpn.NewSessionID()
			if err != nil {
				return err
			}
			if err := a.VPN.Connect(name, nextSessionID); err != nil {
				continue
			}
			sessionID = nextSessionID
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (a *Application) credentialsCommand(args []string) error {
	if len(args) != 2 || args[0] != "set" {
		return errors.New("usage: tunneldesk credentials set <profile>")
	}
	p, err := a.Profiles.Get(args[1])
	if err != nil {
		return err
	}
	reader := bufio.NewReader(a.In)
	fmt.Fprint(a.Out, "Username: ")
	username, _ := reader.ReadString('\n')
	fmt.Fprint(a.Out, "Password: ")
	password, err := secretinput.Read()
	fmt.Fprintln(a.Out)
	if err != nil {
		return fmt.Errorf("read password securely: %w", err)
	}
	if err := a.Vault.Put(p.Name, vault.Credentials{
		Username: strings.TrimSpace(username),
		Password: password,
	}); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Credentials encrypted locally with Windows DPAPI.")
	return nil
}

func (a *Application) autostartCommand(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: tunneldesk autostart <enable|disable> --profile")
	}
	name, err := profileArgument(args[1:])
	if err != nil {
		return err
	}
	p, err := a.Profiles.Get(name)
	if err != nil {
		return err
	}
	switch args[0] {
	case "enable":
		err = autostart.Enable(p.Name)
		p.AutoConnect = err == nil
	case "disable":
		err = autostart.Disable(p.Name)
		p.AutoConnect = false
	default:
		return errors.New("autostart action must be enable or disable")
	}
	if err != nil {
		return err
	}
	if err := a.Profiles.Save(p); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "Autostart %s for %q.\n", args[0]+"d", p.Name)
	return nil
}

func (a *Application) autoConnect(name string) error {
	p, err := a.Profiles.Get(name)
	if err != nil {
		return err
	}
	delay := 2 * time.Second
	var lastErr error
	for attempt := 0; attempt < 12; attempt++ {
		if attempt > 0 {
			time.Sleep(delay)
			if delay < 30*time.Second {
				delay *= 2
			}
		}
		if _, lastErr = a.startRunner(p.Name); lastErr == nil {
			return nil
		}
		state, statusErr := a.VPN.Status(p.Name)
		if statusErr == nil &&
			(state.Status == openvpn.StatusConnected || state.Status == openvpn.StatusConnecting) {
			return nil
		}
	}
	return fmt.Errorf("autoconnect %q failed after 12 attempts: %w", p.Name, lastErr)
}

const help = `TunnelDesk — local OpenVPN profile manager

Usage:
  tunneldesk                         Open the visual interface
  tunneldesk profile add NAME --config PATH
  tunneldesk profile remove NAME
  tunneldesk credentials set NAME
  tunneldesk profiles
  tunneldesk connect --NAME
  tunneldesk disconnect --NAME
  tunneldesk status --NAME
  tunneldesk logs --NAME
  tunneldesk autostart enable --NAME
  tunneldesk autostart disable --NAME
`
