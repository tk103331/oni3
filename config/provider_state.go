package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/tk103331/oni3/internal/datadir"
)

// Serializes read/modify/rename operations within this process. Config mutation
// belongs to the caller's main goroutine, just like the other Config maps.
var providerStateMu sync.Mutex

const maxProviderStateBytes = 4 << 20

type providerState struct {
	Providers map[string]savedProvider `json:"providers"`
}

type savedProvider struct {
	Identity string   `json:"identity"`
	APIKey   string   `json:"api_key,omitempty"`
	Models   []string `json:"models,omitempty"`
}

func stateHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func providerIdentity(p Provider) string {
	// Discovery endpoints can also receive the key, so changing one invalidates
	// saved credentials as well as changing the inference endpoint/type.
	return stateHash(p.EffectiveType() + "\x00" + p.BaseURL + "\x00" + p.ModelsURL)
}

func (c *Config) providerStatePath() (string, error) {
	if c == nil || c.SourcePath == "" {
		return "", fmt.Errorf("provider persistence requires a configuration source path")
	}
	source, err := filepath.Abs(c.SourcePath)
	if err != nil {
		return "", fmt.Errorf("cannot resolve configuration source path")
	}
	data := datadir.Dir()
	if data == "" {
		return "", fmt.Errorf("cannot locate provider state directory")
	}
	return filepath.Join(data, "state", "providers", stateHash(source)+".json"), nil
}

func readProviderState(filename string) (providerState, error) {
	state := providerState{Providers: make(map[string]savedProvider)}
	file, err := os.Open(filename)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("cannot read saved provider state")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxProviderStateBytes+1))
	if err != nil || len(data) > maxProviderStateBytes {
		return state, fmt.Errorf("cannot read saved provider state: invalid size or read failure")
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("saved provider state is invalid JSON")
	}
	if state.Providers == nil {
		state.Providers = make(map[string]savedProvider)
	}
	return state, nil
}

func writeProviderState(filename string, state providerState) error {
	data, err := json.Marshal(state)
	if err != nil || len(data) > maxProviderStateBytes {
		return fmt.Errorf("cannot encode provider state within size limit")
	}
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create provider state directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("cannot secure provider state directory")
	}
	file, err := os.CreateTemp(dir, ".providers-*")
	if err != nil {
		return fmt.Errorf("cannot create provider state file")
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return fmt.Errorf("cannot secure provider state file")
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("cannot write provider state")
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("cannot sync provider state")
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("cannot close provider state")
	}
	if err := os.Rename(tmp, filename); err != nil {
		return fmt.Errorf("cannot replace provider state")
	}
	return nil
}

func validRemoteModelID(id string) bool {
	if id == "" || len(id) > 1024 || !utf8.ValidString(id) || strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func (c *Config) loadProviderState() error {
	if c.SourcePath == "" {
		return nil
	}
	providerStateMu.Lock()
	defer providerStateMu.Unlock()
	filename, err := c.providerStatePath()
	if err != nil {
		return err
	}
	state, err := readProviderState(filename)
	if err != nil {
		return err
	}
	for name, provider := range c.Providers {
		saved, ok := state.Providers[name]
		if !ok || saved.Identity != providerIdentity(provider) {
			continue
		}
		if provider.APIKey == "" {
			provider.APIKey = saved.APIKey
			c.Providers[name] = provider
		}
		// A user may have given a previously imported model a YAML alias.
		// Reuse it rather than recreating a duplicate canonical entry.
		existingIDs := make(map[string]bool)
		for _, model := range c.Models {
			if model.Provider == name {
				existingIDs[model.Model] = true
			}
		}
		for _, id := range saved.Models {
			if !validRemoteModelID(id) {
				return fmt.Errorf("saved provider state contains an invalid model ID")
			}
			if existingIDs[id] {
				continue
			}
			key := name + "/" + id
			if _, exists := c.Models[key]; !exists {
				if c.Models == nil {
					c.Models = make(map[string]Model)
				}
				c.Models[key] = Model{Provider: name, Model: id}
			}
		}
	}
	return nil
}

// SetProviderAPIKey saves a successfully verified key before updating memory.
// An explicit key in the source YAML/plugin continues to win on the next load.
func (c *Config) SetProviderAPIKey(providerName, key string) error {
	providerStateMu.Lock()
	defer providerStateMu.Unlock()
	if c == nil {
		return fmt.Errorf("configuration is required")
	}
	provider, ok := c.Providers[providerName]
	if !ok {
		return fmt.Errorf("unknown provider %q", providerName)
	}
	if err := ValidateProviderType(provider.EffectiveType()); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n\x00") {
		return fmt.Errorf("API key must be nonempty and contain no line breaks")
	}
	filename, err := c.providerStatePath()
	if err != nil {
		return err
	}
	state, err := readProviderState(filename)
	if err != nil {
		return err
	}
	saved := state.Providers[providerName]
	identity := providerIdentity(provider)
	if saved.Identity != identity {
		saved = savedProvider{Identity: identity}
	}
	if saved.APIKey != key {
		saved.APIKey = key
		state.Providers[providerName] = saved
		if err := writeProviderState(filename, state); err != nil {
			return err
		}
	}
	provider.APIKey = key
	c.Providers[providerName] = provider
	return nil
}

// AddProviderModels imports a batch atomically and returns usable model names in
// input order. Existing aliases and their generation settings are preserved.
func (c *Config) AddProviderModels(providerName string, ids []string) ([]string, error) {
	providerStateMu.Lock()
	defer providerStateMu.Unlock()
	if c == nil {
		return nil, fmt.Errorf("configuration is required")
	}
	provider, ok := c.Providers[providerName]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", providerName)
	}
	if err := ValidateProviderType(provider.EffectiveType()); err != nil {
		return nil, err
	}
	aliases := make([]string, 0, len(c.Models))
	for name := range c.Models {
		aliases = append(aliases, name)
	}
	sort.Strings(aliases)
	existing := make(map[string]string)
	for _, name := range aliases {
		model := c.Models[name]
		if model.Provider == providerName {
			if _, ok := existing[model.Model]; !ok {
				existing[model.Model] = name
			}
		}
	}
	result := make([]string, 0, len(ids))
	additions := make(map[string]Model)
	newIDs := make([]string, 0, len(ids))
	seen := make(map[string]bool)
	for _, id := range ids {
		if !validRemoteModelID(id) {
			return nil, fmt.Errorf("invalid model ID")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if alias, ok := existing[id]; ok {
			result = append(result, alias)
			continue
		}
		name := providerName + "/" + id
		if _, exists := c.Models[name]; exists {
			return nil, fmt.Errorf("model name %q is already configured for another model", name)
		}
		additions[name] = Model{Provider: providerName, Model: id}
		newIDs = append(newIDs, id)
		result = append(result, name)
	}
	if len(additions) == 0 {
		return result, nil
	}
	filename, err := c.providerStatePath()
	if err != nil {
		return nil, err
	}
	state, err := readProviderState(filename)
	if err != nil {
		return nil, err
	}
	saved := state.Providers[providerName]
	identity := providerIdentity(provider)
	changed := saved.Identity != identity
	if changed {
		saved = savedProvider{Identity: identity}
	}
	savedIDs := make(map[string]bool, len(saved.Models))
	for _, id := range saved.Models {
		savedIDs[id] = true
	}
	for _, id := range newIDs {
		if !savedIDs[id] {
			saved.Models = append(saved.Models, id)
			savedIDs[id] = true
			changed = true
		}
	}
	if changed {
		state.Providers[providerName] = saved
		if err := writeProviderState(filename, state); err != nil {
			return nil, err
		}
	}
	if c.Models == nil {
		c.Models = make(map[string]Model)
	}
	for name, model := range additions {
		c.Models[name] = model
	}
	return result, nil
}
