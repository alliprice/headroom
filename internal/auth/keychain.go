package auth

// GetAccessToken retrieves the Claude access token by trying each
// credential provider in order: env var, credentials file, macOS keychain.
func GetAccessToken() (string, error) {
	return getTokenFromChain(defaultChain)
}
