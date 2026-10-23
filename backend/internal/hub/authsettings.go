package hub

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"glowpod/internal/auth"
)

// Sign-in settings set in the Hub's UI: SSO (OIDC) and LDAP. They're kept
// in one Secret, client secret and bind password included, and take effect
// at once. Whatever is set with flags or Helm values wins and isn't
// editable here.

const (
	authSecret     = "glowpod-auth" //nolint:gosec // a Secret's name, not a credential
	keyAuth        = "settings"
	checkTimeout   = 10 * time.Second
	oidcUserPrefix = "oidc:"
	ldapUserPrefix = "ldap:"
)

// OIDCSettings are SSO as set in the UI.
type OIDCSettings struct {
	Enabled bool `json:"enabled"`
	// Name labels the sign-in button, such as Okta.
	Name     string `json:"name"`
	Issuer   string `json:"issuer"`
	ClientID string `json:"clientId"`
	// ClientSecret is never sent back; empty on a save keeps the one kept.
	ClientSecret    string   `json:"clientSecret,omitempty"`
	ClientSecretSet bool     `json:"clientSecretSet"`
	Scopes          []string `json:"scopes"`
	UsernameClaim   string   `json:"usernameClaim"`
	GroupsClaim     string   `json:"groupsClaim"`
}

// LDAPSettings are LDAP or Active Directory as set in the UI.
type LDAPSettings struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	BindDN  string `json:"bindDN"`
	// BindPassword is never sent back; empty on a save keeps the one kept.
	BindPassword      string `json:"bindPassword,omitempty"`
	BindPasswordSet   bool   `json:"bindPasswordSet"`
	UserBase          string `json:"userBase"`
	UserFilter        string `json:"userFilter"`
	UsernameAttribute string `json:"usernameAttribute"`
	GroupBase         string `json:"groupBase"`
	GroupFilter       string `json:"groupFilter"`
	GroupAttribute    string `json:"groupAttribute"`
	StartTLS          bool   `json:"startTLS"`
	// CA is a PEM CA to trust for ldaps:// or StartTLS.
	CA string `json:"ca"`
}

// AuthSettings are both, as kept.
type AuthSettings struct {
	OIDC OIDCSettings `json:"oidc"`
	LDAP LDAPSettings `json:"ldap"`
}

func defaultAuth() AuthSettings {
	return AuthSettings{
		OIDC: OIDCSettings{Name: "SSO", Scopes: []string{"openid", "email", "profile"}, UsernameClaim: "email", GroupsClaim: "groups"},
		LDAP: LDAPSettings{UserFilter: "(uid={username})", UsernameAttribute: "uid", GroupFilter: "(member={dn})", GroupAttribute: "cn"},
	}
}

func (s *store) authSettings(ctx context.Context) (AuthSettings, error) {
	data, err := s.secret(ctx, authSecret)
	if err != nil {
		return AuthSettings{}, err
	}
	out := defaultAuth()
	if data == nil {
		return out, nil
	}
	if err := json.Unmarshal(data[keyAuth], &out); err != nil {
		return AuthSettings{}, fmt.Errorf("read the sign-in settings: %w", err)
	}
	return out, nil
}

func (s *store) saveAuthSettings(ctx context.Context, a AuthSettings) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("encode the sign-in settings: %w", err)
	}
	return s.putSecret(ctx, authSecret, "auth", map[string][]byte{keyAuth: raw})
}

// FixedAuth is sign-in set with flags or Helm values, which the UI can't
// change.
type FixedAuth struct {
	OIDC     *auth.OIDC
	OIDCName string
	LDAP     *auth.LDAP
}

// UseAuth gives the Hub the sign-in server's SetProviders and what flags
// set, and applies the kept settings.
func (h *Hub) UseAuth(ctx context.Context, set func(*auth.OIDC, string, *auth.LDAP), fixed FixedAuth) {
	h.mu.Lock()
	h.setProviders, h.fixedAuth = set, fixed
	h.mu.Unlock()
	if err := h.applyAuth(ctx); err != nil {
		h.cfg.Logger.Warn("couldn't apply the saved sign-in settings; fix them under Hub settings", "err", err)
	}
}

// applyAuth builds SSO and LDAP from the kept settings, except where flags
// set them, and hands them to the sign-in server.
func (h *Hub) applyAuth(ctx context.Context) error {
	h.mu.Lock()
	set, fixed := h.setProviders, h.fixedAuth
	h.mu.Unlock()
	if set == nil {
		return nil
	}
	a, err := h.store.authSettings(ctx)
	if err != nil {
		set(fixed.OIDC, fixed.OIDCName, fixed.LDAP)
		return err
	}
	o, name, l := fixed.OIDC, fixed.OIDCName, fixed.LDAP
	var errs []error
	if o == nil && a.OIDC.Enabled {
		built, err := h.buildOIDC(ctx, a.OIDC)
		if err != nil {
			errs = append(errs, err)
		} else {
			o, name = built, a.OIDC.Name
		}
	}
	if l == nil && a.LDAP.Enabled {
		built, err := buildLDAP(a.LDAP)
		if err != nil {
			errs = append(errs, err)
		} else {
			l = built
		}
	}
	set(o, name, l)
	return errors.Join(errs...)
}

func (h *Hub) buildOIDC(ctx context.Context, s OIDCSettings) (*auth.OIDC, error) {
	serverURL, err := h.serverURL(ctx)
	if err != nil {
		return nil, err
	}
	if err := checkIssuer(s.Issuer); err != nil {
		return nil, err
	}
	o, err := auth.NewOIDC(auth.OIDCConfig{
		Issuer:         s.Issuer,
		ClientID:       s.ClientID,
		ClientSecret:   s.ClientSecret,
		RedirectURL:    serverURL + auth.OIDCCallbackPath,
		Scopes:         s.Scopes,
		UsernameClaim:  s.UsernameClaim,
		GroupsClaim:    s.GroupsClaim,
		UsernamePrefix: oidcUserPrefix,
		GroupsPrefix:   oidcUserPrefix,
	})
	if err != nil {
		return nil, fmt.Errorf("SSO: %w", err)
	}
	return o, nil
}

func checkIssuer(issuer string) error {
	u, err := url.Parse(issuer)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errors.New("the issuer is an http(s) URL, such as https://login.example.com")
	}
	return nil
}

func buildLDAP(s LDAPSettings) (*auth.LDAP, error) {
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "ldap" && u.Scheme != "ldaps") || u.Host == "" {
		return nil, errors.New("the LDAP URL is ldap:// or ldaps://, such as ldaps://ldap.example.com")
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
	if strings.TrimSpace(s.CA) != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(s.CA)) {
			return nil, errors.New("the CA isn't a PEM certificate")
		}
		tlsCfg.RootCAs = pool
	}
	l, err := auth.NewLDAP(auth.LDAPConfig{
		URL:               s.URL,
		BindDN:            s.BindDN,
		BindPassword:      s.BindPassword,
		UserBase:          s.UserBase,
		UserFilter:        s.UserFilter,
		UsernameAttribute: s.UsernameAttribute,
		UsernamePrefix:    ldapUserPrefix,
		GroupBase:         s.GroupBase,
		GroupFilter:       s.GroupFilter,
		GroupAttribute:    s.GroupAttribute,
		GroupsPrefix:      ldapUserPrefix,
		StartTLS:          s.StartTLS,
		TLS:               tlsCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("LDAP: %w", err)
	}
	return l, nil
}

// API, for Hub administrators.

// settingsResponse is GET /api/v1/hub/settings.
type settingsResponse struct {
	ServerURL string       `json:"serverURL"`
	Auth      AuthSettings `json:"auth"`
	// Fixed are the methods set with flags or Helm values.
	Fixed struct {
		OIDC bool `json:"oidc"`
		LDAP bool `json:"ldap"`
	} `json:"fixed"`
	// RedirectURL is what to register with the SSO provider.
	RedirectURL string `json:"redirectURL"`
}

func (h *Hub) getSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, err := h.store.authSettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "settings_failed", "Couldn't read the settings.")
		return
	}
	serverURL, _ := h.serverURL(ctx)
	out := settingsResponse{ServerURL: serverURL, Auth: redact(a), RedirectURL: serverURL + auth.OIDCCallbackPath}
	h.mu.Lock()
	out.Fixed.OIDC, out.Fixed.LDAP = h.fixedAuth.OIDC != nil, h.fixedAuth.LDAP != nil
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

func redact(a AuthSettings) AuthSettings {
	a.OIDC.ClientSecretSet, a.OIDC.ClientSecret = a.OIDC.ClientSecret != "", ""
	a.LDAP.BindPasswordSet, a.LDAP.BindPassword = a.LDAP.BindPassword != "", ""
	return a
}

func (h *Hub) setServerURL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerURL string `json:"serverURL"`
	}
	if !decode(w, r, &req) {
		return
	}
	u, ok := checkServerURL(req.ServerURL)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_server_url", "The server URL is an http(s) address without a path, such as https://glowpod.example.com.")
		return
	}
	ctx := r.Context()
	set, err := h.store.settings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "settings_failed", "Couldn't save the server URL.")
		return
	}
	set.ServerURL = u
	if err := h.store.saveSettings(ctx, set); err != nil {
		writeError(w, http.StatusInternalServerError, "settings_failed", "Couldn't save the server URL.")
		return
	}
	// SSO's redirect address follows the server URL.
	if err := h.applyAuth(ctx); err != nil {
		h.cfg.Logger.Warn("sign-in settings", "err", err)
	}
	h.cfg.Logger.Info("server URL changed", "by", auth.SessionInfoFrom(ctx).User, "server_url", u)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) fixed(which string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if which == "oidc" {
		return h.fixedAuth.OIDC != nil
	}
	return h.fixedAuth.LDAP != nil
}

func (h *Hub) setOIDC(w http.ResponseWriter, r *http.Request) {
	var req OIDCSettings
	if !decode(w, r, &req) {
		return
	}
	if h.fixed("oidc") {
		writeError(w, http.StatusConflict, "fixed", "SSO is set with Helm values (or flags); change it there.")
		return
	}
	ctx := r.Context()
	a, err := h.store.authSettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "settings_failed", "Couldn't read the settings.")
		return
	}
	if req.ClientSecret == "" {
		req.ClientSecret = a.OIDC.ClientSecret
	}
	req.ClientSecretSet = false
	if req.Name = strings.TrimSpace(req.Name); req.Name == "" {
		req.Name = "SSO"
	}
	if req.Enabled {
		if _, err := h.buildOIDC(ctx, req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_settings", plain(err))
			return
		}
	}
	a.OIDC = req
	h.saveAndApply(w, r, a, "SSO")
}

func (h *Hub) setLDAP(w http.ResponseWriter, r *http.Request) {
	var req LDAPSettings
	if !decode(w, r, &req) {
		return
	}
	if h.fixed("ldap") {
		writeError(w, http.StatusConflict, "fixed", "LDAP is set with Helm values (or flags); change it there.")
		return
	}
	ctx := r.Context()
	a, err := h.store.authSettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "settings_failed", "Couldn't read the settings.")
		return
	}
	if req.BindPassword == "" {
		req.BindPassword = a.LDAP.BindPassword
	}
	req.BindPasswordSet = false
	if req.Enabled {
		if _, err := buildLDAP(req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_settings", plain(err))
			return
		}
	}
	a.LDAP = req
	h.saveAndApply(w, r, a, "LDAP")
}

func (h *Hub) saveAndApply(w http.ResponseWriter, r *http.Request, a AuthSettings, what string) {
	ctx := r.Context()
	if err := h.store.saveAuthSettings(ctx, a); err != nil {
		writeError(w, http.StatusInternalServerError, "settings_failed", "Couldn't save the settings.")
		return
	}
	if err := h.applyAuth(ctx); err != nil {
		writeError(w, http.StatusBadRequest, "bad_settings", plain(err))
		return
	}
	h.cfg.Logger.Info("sign-in settings changed", "by", auth.SessionInfoFrom(ctx).User, "method", what,
		"enabled", (what == "SSO" && a.OIDC.Enabled) || (what == "LDAP" && a.LDAP.Enabled))
	w.WriteHeader(http.StatusNoContent)
}

// checkOIDC reaches the provider with settings not yet saved.
func (h *Hub) checkOIDC(w http.ResponseWriter, r *http.Request) {
	var req OIDCSettings
	if !decode(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
	defer cancel()
	o, err := h.buildOIDC(ctx, req)
	if err == nil {
		err = o.Check(ctx)
	}
	h.checkResult(w, err, "Reached "+req.Issuer+" and read its OpenID configuration.")
}

// checkLDAP connects and binds as the service account, with settings not
// yet saved.
func (h *Hub) checkLDAP(w http.ResponseWriter, r *http.Request) {
	var req LDAPSettings
	if !decode(w, r, &req) {
		return
	}
	if req.BindPassword == "" {
		if a, err := h.store.authSettings(r.Context()); err == nil {
			req.BindPassword = a.LDAP.BindPassword
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
	defer cancel()
	l, err := buildLDAP(req)
	if err == nil {
		err = l.Check(ctx)
	}
	ok := "Connected to " + req.URL
	if req.BindDN != "" {
		ok += " and signed in as " + req.BindDN
	}
	h.checkResult(w, err, ok+".")
}

func (h *Hub) checkResult(w http.ResponseWriter, err error, ok string) {
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": plain(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": ok})
}

// plain makes an error a sentence for the settings page.
func plain(err error) string {
	msg := err.Error()
	if msg == "" {
		return "Something went wrong."
	}
	msg = strings.ToUpper(msg[:1]) + msg[1:]
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg
}
