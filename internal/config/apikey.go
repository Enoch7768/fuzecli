package config

import (
	"errors"
	"fmt"
	"strings"
)

var supportedAPIKeyProviders = map[string]bool{
	"openai": true,
	"gemini": true,
	"groq": true,
	"anthropic": true,
}

func SetAPIKey(providerName, key string) error {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	key = strings.TrimSpace(key)
	if !supportedAPIKeyProviders[providerName] {
		return fmt.Errorf("unsupported provider %q", providerName)
	}
	if key == "" {
		return errors.New("API key cannot be empty")
	}
	return Set("providers."+providerName+".api_key", key)
}

func ClearAPIKey(providerName string) error {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	if !supportedAPIKeyProviders[providerName] {
		return fmt.Errorf("unsupported provider %q", providerName)
	}
	c, err := Load()
	if err != nil {
		return err
	}
	p := c.Providers[providerName]
	p.APIKey = ""
	c.Providers[providerName] = p
	return Save(c)
}

func APIKeyStatus() (map[string]bool, error) {
	c, err := Load()
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(supportedAPIKeyProviders))
	for name := range supportedAPIKeyProviders {
		result[name] = strings.TrimSpace(c.Providers[name].APIKey) != ""
	}
	return result, nil
}
