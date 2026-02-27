package auth

import "fmt"

// defaultChain is the provider order: env override, then credentials file,
// then macOS keychain. First successful token wins.
var defaultChain = []credentialProvider{
	envProvider{},
	fileProvider{},
	keychainProvider{},
}

func getTokenFromChain(chain []credentialProvider) (string, error) {
	var lastErr error
	for _, p := range chain {
		token, err := p.getToken()
		if err == nil {
			return token, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", fmt.Errorf("all credential providers failed (last: %w)", lastErr)
	}
	return "", fmt.Errorf("no credential providers configured")
}
