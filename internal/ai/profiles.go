package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	secretbox "github.com/kaanemec/things-items-inventory/internal/secret"
)

// Role says what an endpoint is for.
type Role string

const (
	RoleVision Role = "vision" // photo understanding
	RoleText   Role = "text"   // everything else
)

// ParseRole accepts "vision" and "text".
func ParseRole(s string) (Role, bool) {
	r := Role(s)
	return r, r == RoleVision || r == RoleText
}

// DefaultBaseURL is the base URL of a new profile.
const DefaultBaseURL = "https://openrouter.ai/api/v1"

const stampLayout = "2006-01-02T15:04:05.000Z"

// NotSetUpError means the role has no usable endpoint: no profile of the
// household, no server default, or no model chosen. Test with
// errors.Is(err, ErrNotSetUp) or errors.As.
type NotSetUpError struct{ Role Role }

// ErrNotSetUp is what every *NotSetUpError matches with errors.Is.
var ErrNotSetUp = errors.New("ai: not set up")

func (e *NotSetUpError) Error() string        { return "ai: " + string(e.Role) + " endpoint is not set up" }
func (e *NotSetUpError) Is(target error) bool { return target == ErrNotSetUp }

// Capabilities is what the last connection test found (J5.3).
type Capabilities struct {
	Images     bool `json:"images"`
	JSONSchema bool `json:"json_schema"`
}

// Service owns the instance key and the server-wide defaults.
type Service struct {
	d   *db.DB
	cfg config.Config

	client *Client // for connection tests and model lists

	mu  sync.Mutex
	box *secretbox.Box

	testLimit, modelsLimit limiter
}

var current atomic.Pointer[Service]

// NewService creates the service and makes it the one behind For. A server has one.
// When a key source exists (data directory or INVENTORY_SECRET_KEY) the key
// is loaded, and created if needed, right away; otherwise on first use.
func NewService(d *db.DB, cfg config.Config) *Service {
	s := &Service{d: d, cfg: cfg, client: New(NewHTTPClient(cfg.AIBlockPrivate)),
		testLimit: limiter{max: 10, per: time.Minute}, modelsLimit: limiter{max: 30, per: time.Minute}}
	if cfg.DataDir != "" || cfg.SecretKey != "" {
		if _, err := s.key(); err != nil {
			slog.Error("ai: secret key", "err", err)
		}
	}
	current.Store(s)
	return s
}

func (s *Service) key() (*secretbox.Box, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.box != nil {
		return s.box, nil
	}
	b, err := secretbox.Load(s.cfg.DataDir, string(s.cfg.SecretKey))
	if err != nil {
		return nil, err
	}
	s.box = b
	return b, nil
}

// aad binds a ciphertext to its row, so it cannot be copied to another
// household or role.
func aad(householdID string, role Role) []byte {
	return []byte("ai_profiles.token\x00" + householdID + "\x00" + string(role))
}

func (s *Service) seal(token, householdID string, role Role) ([]byte, error) {
	b, err := s.key()
	if err != nil {
		return nil, err
	}
	return b.Seal([]byte(token), aad(householdID, role))
}

func (s *Service) open(row sqlc.AiProfile) (string, error) {
	if len(row.TokenEnc) == 0 {
		return "", nil
	}
	b, err := s.key()
	if err != nil {
		return "", err
	}
	pt, err := b.Open(row.TokenEnc, aad(row.HouseholdID, Role(row.Role)))
	if err != nil {
		return "", fmt.Errorf("ai: stored %s token cannot be decrypted (wrong secret key?)", row.Role)
	}
	return string(pt), nil
}

// serverDefault returns the environment defaults for role, with the base URL
// normalised, and whether any are set.
func (s *Service) serverDefault(role Role) (config.AIEndpoint, bool) {
	e := s.cfg.AI.Vision
	if role == RoleText {
		e = s.cfg.AI.Text
	}
	if e.BaseURL == "" && e.Token == "" && e.Model == "" {
		return e, false
	}
	if e.BaseURL == "" {
		e.BaseURL = DefaultBaseURL
	} else if n, err := NormalizeBaseURL(e.BaseURL); err == nil {
		e.BaseURL = n
	} else {
		slog.Warn("ai: ignoring invalid server default base URL", "role", role, "err", err)
		e.BaseURL = DefaultBaseURL
	}
	return e, true
}

// For returns the endpoint configuration to use for role in a household:
//
//   - the household's own profile, if it has one;
//   - for text with same_as_vision on (the default), the vision profile;
//   - otherwise the server-wide defaults from the environment.
//
// It returns a *NotSetUpError (errors.Is(err, ErrNotSetUp)) when there is no
// endpoint with a model. q may sit on a transaction or the read pool. The
// token is in the returned Config only: never log it.
func For(ctx context.Context, q *sqlc.Queries, householdID string, role Role) (Config, error) {
	s := current.Load()
	if s == nil {
		return Config{}, errors.New("ai: service not initialised")
	}
	return s.For(ctx, q, householdID, role)
}

// For is the package-level For on this service.
func (s *Service) For(ctx context.Context, q *sqlc.Queries, householdID string, role Role) (Config, error) {
	if _, ok := ParseRole(string(role)); !ok {
		return Config{}, fmt.Errorf("ai: unknown role %q", role)
	}
	cfg, err := s.resolve(ctx, q, householdID, role)
	var ns *NotSetUpError
	if errors.As(err, &ns) {
		return Config{}, &NotSetUpError{Role: role}
	}
	return cfg, err
}

func (s *Service) resolve(ctx context.Context, q *sqlc.Queries, householdID string, role Role) (Config, error) {
	row, err := q.GetAIProfile(ctx, sqlc.GetAIProfileParams{HouseholdID: householdID, Role: string(role)})
	if errors.Is(err, sql.ErrNoRows) {
		if e, ok := s.serverDefault(role); ok {
			if e.Model == "" {
				return Config{}, &NotSetUpError{Role: role}
			}
			return Config{BaseURL: e.BaseURL, Token: string(e.Token), Model: e.Model}, nil
		}
		if role == RoleText { // same_as_vision is on until a text profile says otherwise
			return s.resolve(ctx, q, householdID, RoleVision)
		}
		return Config{}, &NotSetUpError{Role: role}
	}
	if err != nil {
		return Config{}, err
	}
	if role == RoleText && row.SameAsVision != 0 {
		return s.resolve(ctx, q, householdID, RoleVision)
	}
	if !row.Model.Valid || row.Model.String == "" {
		return Config{}, &NotSetUpError{Role: role}
	}
	token, err := s.open(row)
	if err != nil {
		return Config{}, err
	}
	return Config{BaseURL: row.BaseUrl, Token: token, Model: row.Model.String, Mode: modeFromStored(row.OutputMode.String)}, nil
}

func modeFromStored(s string) Mode {
	switch s {
	case "json_schema":
		return ModeStrict
	case "json_object":
		return ModeJSONObject
	case "prompt":
		return ModePrompt
	}
	return ModeAuto
}

func modeToStored(m Mode) sql.NullString {
	switch m {
	case ModeStrict:
		return sql.NullString{String: "json_schema", Valid: true}
	case ModeJSONObject:
		return sql.NullString{String: "json_object", Valid: true}
	case ModePrompt:
		return sql.NullString{String: "prompt", Valid: true}
	}
	return sql.NullString{}
}

// TestOutcome is what a connection test (J5.3) saves on a profile.
type TestOutcome struct {
	Mode         Mode // the structured-output mode that worked; ModeAuto/empty stores none
	Capabilities Capabilities
	Result       json.RawMessage // step list; nil stores none
	At           time.Time
}

// SaveTest records a test outcome on the household's saved profile and clears
// "failing". A role that uses server defaults has no row, so nothing changes.
func SaveTest(ctx context.Context, q *sqlc.Queries, householdID string, role Role, t TestOutcome) error {
	caps, err := json.Marshal(t.Capabilities)
	if err != nil {
		return err
	}
	res := sql.NullString{}
	if len(t.Result) > 0 {
		res = sql.NullString{String: string(t.Result), Valid: true}
	}
	return q.SaveAITest(ctx, sqlc.SaveAITestParams{
		OutputMode: modeToStored(t.Mode), Capabilities: string(caps), TestResult: res,
		TestedAt:    sql.NullString{String: t.At.UTC().Format(stampLayout), Valid: true},
		HouseholdID: householdID, Role: string(role)})
}

// MarkFailing notes that the endpoint started failing at `at` (kept if it
// already was failing). ClearFailing undoes it.
func MarkFailing(ctx context.Context, q *sqlc.Queries, householdID string, role Role, at time.Time) error {
	return q.MarkAIFailing(ctx, sqlc.MarkAIFailingParams{
		FailingSince: sql.NullString{String: at.UTC().Format(stampLayout), Valid: true},
		HouseholdID:  householdID, Role: string(role)})
}

// ClearFailing clears the failing marker.
func ClearFailing(ctx context.Context, q *sqlc.Queries, householdID string, role Role) error {
	return q.ClearAIFailing(ctx, sqlc.ClearAIFailingParams{HouseholdID: householdID, Role: string(role)})
}

// NormalizeBaseURL validates a base URL and returns it in canonical form:
// http or https only, a host, no credentials, query or fragment, and neither
// a trailing slash nor a trailing /chat/completions. The error never repeats
// the input (it may hold a password).
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("Enter a base URL.")
	}
	if len(raw) > 2048 {
		return "", errors.New("That URL is too long.")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("That is not a valid URL.")
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme != "http" && scheme != "https":
		return "", errors.New("The URL must start with http:// or https://.")
	case u.Opaque != "" || u.Hostname() == "":
		return "", errors.New("The URL needs a host name.")
	case u.User != nil:
		return "", errors.New("Do not put a user name or password in the URL; use the token field.")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#"):
		return "", errors.New("The URL must not contain a query or fragment.")
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	path = strings.TrimSuffix(path, "/chat/completions")
	path = strings.TrimRight(path, "/")
	return scheme + "://" + strings.ToLower(u.Host) + path, nil
}

// saved is what a household would use for a role right now, without the
// requirement that a model is chosen.
type saved struct {
	cfg     Config
	effRole Role // the role whose settings these are (text may use vision's)
	hasRow  bool // a household row supplies them (false: server defaults or nothing)
}

func (s *Service) gather(ctx context.Context, q *sqlc.Queries, householdID string, role Role) (saved, error) {
	row, err := q.GetAIProfile(ctx, sqlc.GetAIProfileParams{HouseholdID: householdID, Role: string(role)})
	if errors.Is(err, sql.ErrNoRows) {
		if e, ok := s.serverDefault(role); ok {
			return saved{cfg: Config{BaseURL: e.BaseURL, Token: string(e.Token), Model: e.Model}, effRole: role}, nil
		}
		if role == RoleText {
			return s.gather(ctx, q, householdID, RoleVision)
		}
		return saved{cfg: Config{BaseURL: DefaultBaseURL}, effRole: role}, nil
	}
	if err != nil {
		return saved{}, err
	}
	if role == RoleText && row.SameAsVision != 0 {
		return s.gather(ctx, q, householdID, RoleVision)
	}
	token, err := s.open(row)
	if err != nil {
		return saved{}, err
	}
	return saved{cfg: Config{BaseURL: row.BaseUrl, Token: token, Model: row.Model.String, Mode: modeFromStored(row.OutputMode.String)},
		effRole: role, hasRow: true}, nil
}

// NoteResult records how a real AI request went, for the profile that served
// it: an auth, model-not-found or out-of-credit error marks it failing; a
// success clears the mark. Other errors change nothing. q must be on the
// write connection. Errors of its own are only logged.
func NoteResult(ctx context.Context, q *sqlc.Queries, householdID string, role Role, callErr error) {
	s := current.Load()
	if s == nil {
		return
	}
	sv, err := s.gather(ctx, q, householdID, role)
	if err != nil || !sv.hasRow {
		return
	}
	var ae *Error
	switch {
	case callErr == nil:
		err = ClearFailing(ctx, q, householdID, sv.effRole)
	case errors.As(callErr, &ae) && (ae.Kind == KindAuth || ae.Kind == KindModelNotFound || ae.Kind == KindNoCredit):
		err = MarkFailing(ctx, q, householdID, sv.effRole, time.Now())
	}
	if err != nil {
		slog.Warn("ai: note result", "role", sv.effRole, "err", err)
	}
}

// limiter allows max events per key within per.
type limiter struct {
	max int
	per time.Duration
	mu  sync.Mutex
	at  map[string][]time.Time
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.at == nil {
		l.at = map[string][]time.Time{}
	}
	now := time.Now()
	recent := l.at[key][:0]
	for _, t := range l.at[key] {
		if now.Sub(t) < l.per {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.max {
		l.at[key] = recent
		return false
	}
	l.at[key] = append(recent, now)
	return true
}
