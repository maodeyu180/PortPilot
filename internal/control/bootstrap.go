package control

import (
	"fmt"
	"os"
	"path/filepath"
)

// BootstrapOptions only seed a new store. Existing credentials and pending
// changes survive container recreation without being reapplied or reset.
type BootstrapOptions struct {
	PublicHost    string
	ControllerURL string
	Username      string
	Password      string
}

// Bootstrap prepares an empty installation before Mihomo starts. An existing
// applied configuration is always preserved byte for byte.
func Bootstrap(dir string, options BootstrapOptions) error {
	statePath := filepath.Join(dir, "state.json")
	configPath := filepath.Join(dir, "mihomo.yaml")
	_, stateErr := os.Stat(statePath)
	configInfo, configErr := os.Lstat(configPath)
	if configErr != nil && !os.IsNotExist(configErr) {
		return configErr
	}
	if configErr == nil {
		if !configInfo.Mode().IsRegular() {
			return fmt.Errorf("mihomo.yaml 必须是普通文件")
		}
		if os.IsNotExist(stateErr) {
			return fmt.Errorf("已有 mihomo.yaml 但缺少 state.json，请同时恢复两份数据")
		}
	}
	store, err := openStore(dir, func(st *State) error {
		if options.PublicHost != "" {
			st.Settings.PublicHost = options.PublicHost
		}
		if options.ControllerURL != "" {
			st.Settings.ControllerURL = options.ControllerURL
		}
		if options.Username != "" {
			st.Settings.Username = options.Username
		}
		if options.Password != "" {
			st.Settings.Password = options.Password
		}
		return validateSettings(st.Settings)
	})
	if err != nil {
		return err
	}
	if configErr == nil {
		return nil
	}
	st := store.Snapshot()
	if len(st.Nodes) != 0 {
		return fmt.Errorf("已有节点但缺少已应用的 mihomo.yaml，请恢复备份或确认后从面板下载配置")
	}
	config, err := GenerateConfig(st)
	if err != nil {
		return err
	}
	// Publish a complete file without replacing one created concurrently.
	f, err := os.CreateTemp(dir, ".bootstrap-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(config)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Link(f.Name(), configPath); err != nil && !os.IsExist(err) {
		return err
	}
	return nil
}
