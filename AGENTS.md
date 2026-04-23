# agent-deepweb — AGENTS guide

`curl`-with-auth for AI agents. The human stores credentials under names; the LLM invokes them by name. The LLM never sees the secret values.

This file is the entry point for an agent (human or AI) picking up the repo. Read top-to-bottom the first time; skim the section links after that. `CLAUDE.md` is a symlink to this file — there's only one source of truth.

---

## Where to start

1. **Read [README.md](README.md)** for the user-facing pitch: what the tool does, quick-start install + a first request.
2. **Read [skills/agent-deepweb/SKILL.md](skills/agent-deepweb/SKILL.md)** for the LLM-facing contract: which commands are agent-safe vs human-only, how to interpret `fixable_by`, and the exact quickstart the agent skill loads.
3. **Read [design-docs/v1-design.md](design-docs/v1-design.md)** for the v1 design rationale: threat model, credential shape, error-hint catalog. The *Future* section at the bottom lists deferred work.
4. **Skim this file's [Project Structure](#project-structure)** to know where things live.
5. **Then pick your task** — if it's:
   - a **new subcommand** → see [Adding a subcommand](#adding-a-subcommand)
   - a **new auth type** → see [Adding an auth type](#adding-an-auth-type)
   - a **bug in redaction/classification/matching** → go straight to the unit tests in the same package and add a failing case
   - **anything touching HTTP** → spin up `make mock`, set `AGENT_DEEPWEB_CONFIG_DIR=/tmp/somewhere`, and drive the binary against `mockdeep` before writing any production code

Before committing anything: `make fmt && make lint && make test`.

---

## Project Structure

```
cmd/
  agent-deepweb/main.go             Entry point — version stamped via ldflags
  mockdeep/main.go                  Demo server used by e2e tests (see Mock server)
internal/
  api/                              HTTP transport layer
    client.go                       Request/Response/ClientOptions + Do() orchestrator
    request.go                      buildHTTPRequest, resolveUserAgent, viewPersisted
    scheme.go                       enforceScheme, isLoopback (https-only policy)
    classify.go                     classifyTransport, classifyHTTP → fixable_by
    auth.go                         ApplyAuth per credential type
    redact.go                       Header/body/literal-value redactors
  audit/log.go                      Append-only JSONL request log
  cli/                              Cobra command tree
    root.go                         Global flags + subcommand registration
    usage.go                        Top-level llm-help card
    shared/                         Helpers used by every subcommand
      shared.go                     GlobalFlags, IsAgentMode, ResolveAuth
      helpers.go                    Fail, HumanOnlyRunE, First*, SplitHeader, SplitKV, ResolveLimits
    fetch/                          `fetch` — curl-with-auth
    graphql/                        `graphql` — POST + error classification
    creds/                          `creds {list,show,test,add,remove,…}` — split per concern
      creds.go                      Register + list + show
      test.go / add.go / remove.go  One file per subcommand
      domains.go                    allow / disallow / allow-path / disallow-path + mutateSlice
      config.go                     set-health / set-default-header / set-allow-http / set-user-agent
    login/                          `login` + `session …`
      login.go                      Register wiring
      session.go                    session status/show/clear/set-expires/mark-*
      form.go                       doLogin + validateLoginURL + buildLoginBody + …
    tpl/                            `tpl {list,show,run,import,remove}`
    audit/                          `audit tail / summary`
  config/config.go                  App config I/O (~/.config/agent-deepweb/config.json)
  credential/                       Credential storage
    credential.go                   Types (Credential, Secrets, Resolved, indexEntry)
    store.go                        Store/Remove + index+secrets file I/O
    query.go                        List / GetMetadata / Resolve
    mutate.go                       Set*(name, …) setters (used by `creds set-*`)
    match.go                        MatchesURL — host[:port] + path allowlist
    cookie.go                       PersistedCookie + sensitivity classification
    session.go                      Session (cookies + token + expiry) + jar helpers
    keychain.go                     macOS Keychain adapter
    notfound.go                     WrapNotFound for CLI callers
  errors/errors.go                  APIError{Message, Hint, FixableBy, Cause}
  mockdeep/server.go                Mock HTTP server (one handler per auth mode)
  output/                           LLM-facing output
    output.go                       PrintJSON, WriteError, format parsing
    envelope.go                     BuildHTTPEnvelope, RenderBody (shared by fetch + tpl)
  template/                         Request templates (the highest-safety mode)
    template.go                     Template / ParamSpec types + Store/Get/List/Remove/Import
    validate.go                     Type coercion, Required/Default/Enum checks
    substitute.go                   {{param}} rendering (URL path/query, JSON body, lint)
    notfound.go                     WrapNotFound
design-docs/v1-design.md            Design rationale + Future section
skills/agent-deepweb/SKILL.md       Claude Code skill definition
```

---

## Command domains

| Verb | Agent-safe? | Purpose |
|------|-------------|---------|
| `fetch` | ✅ | One-shot authenticated HTTP (curl-with-auth). |
| `graphql` | ✅ | GraphQL POST with classified error envelope. |
| `tpl` | `run` ✅, `import/remove` human-only | **Highest-safety mode.** LLM fills in parameter values only. |
| `creds list/show/test` | ✅ | Read-only credential introspection. |
| `creds add/remove/allow/…/set-*` | ❌ human-only | Credential mutation. |
| `login` | ❌ human-only | Form-login flow producing a session. |
| `session status/show` | ✅ | Session introspection (sensitive values masked). |
| `session clear/set-expires/mark-*` | ❌ human-only | Session mutation. |
| `audit tail/summary` | ✅ | Read the append-only request log. |
| `llm-help` | ✅ | Per-verb reference cards. |

Human-only commands refuse when `AGENT_DEEPWEB_MODE=agent` is set, with `fixable_by: human` and a hint telling the LLM what the human should run.

---

## Key Design Decisions

- **Secrets are names to the LLM.** `fetch --auth <name>` or implicit URL-based resolution — never `--header 'Authorization: Bearer ...'`. The binary never prints credential values.
- **URL allowlist per credential.** `domains` supports `host`, `host:port`, `*.wildcard`, `*.wildcard:port`. Optional `paths` narrows to URL path patterns (exact, `/prefix/*`, or `path.Match` glob). Off-list requests fail `fixable_by: human`.
- **https-only by default.** Bearer/basic/cookie/form credentials refuse `http://` unless the host is loopback, the credential has `allow_http: true` (human-set), or the per-request `--allow-http` human-only flag is set.
- **Cookie jar + session cookies with classification.** Form auth uses `net/http/cookiejar` with the public-suffix list. Each persisted cookie carries a `Sensitive` bool (HttpOnly OR name matches an auth-cookie pattern). `session show` reveals visible cookies with values, sensitive cookies as `<redacted>`. Humans override via `session mark-sensitive` / `mark-visible`.
- **Form-login.** `agent-deepweb login <name>` POSTs form/JSON credentials, harvests Set-Cookie, optionally extracts a Bearer token from the JSON body at `--token-path`, computes expiry as `min(session-ttl, latest-cookie-expiry, 24h)`. The login response body is never returned to the caller.
- **Agent-mode refusals.** `AGENT_DEEPWEB_MODE=agent` makes human-only commands refuse. `session status/show` and `creds list/show/test` stay available.
- **Redact by default.** Response headers and JSON body fields matching secret patterns are `<redacted>`; byte-level substitution also masks literal secret values anywhere they appear in the response. In agent mode, redaction can't be disabled.
- **Per-credential default headers.** Non-secret headers applied to every request using the credential (`Accept: application/vnd.api+json`, API versioning, etc).
- **Keychain on macOS** (service `app.paulie.agent-deepweb`); 0600 file fallback elsewhere (`credentials.secrets.json`). Sessions always on disk at `sessions/<name>.json`, mode 0600.
- **Fixable-by hints everywhere.** Every error is `{error, hint, fixable_by}`. `agent` (fix and retry), `human` (ask user), `retry` (transient).
- **Request templates.** Fixed method, URL (with `{{param}}` placeholders), optional query/headers/body_template, credential binding, typed parameter schema. LLM runs `tpl run <name> --param k=v`; values are coerced and validated *before* any HTTP request. Body substitution is type-preserving.
- **Audit log.** Every fetch/graphql/tpl-run writes one JSONL entry to `~/.config/agent-deepweb/audit.log` with method, host, path, credential name, template name, agent_mode, status, bytes, duration, and `outcome`+`fixable_by` on errors. Never includes bodies, headers, or query strings. Opt out via `AGENT_DEEPWEB_AUDIT=off`.
- **User-Agent precedence.** per-request `--user-agent` > credential `user_agent` > `User-Agent` header > `AGENT_DEEPWEB_USER_AGENT` env > `agent-deepweb/<version>` (curl-style).
- **Single binary, pure Go.** `CGO_ENABLED=0`; deps: cobra, `golang.org/x/net/publicsuffix`.

---

## Dev workflow

```bash
make build          # Build agent-deepweb
make build-mock     # Build mockdeep
make test           # go test ./...
make test-short     # skip slow tests
make vet            # go vet
make fmt            # gofmt (+ goimports if installed)
make lint           # golangci-lint run ./...
make dev ARGS="fetch https://httpbin.org/get"
make mock           # Run mockdeep on :8765
```

**Linter config** lives at `.golangci.yml` (v2 schema, default linters, narrow `errcheck` exclusions for `cobra.MarkFlagRequired` + `_test.go` files). Currently zero issues across the repo.

---

## Mock server (`mockdeep`)

`cmd/mockdeep` is a dumb sibling binary that drives e2e tests. Each auth style has its own endpoint accepting one hardcoded credential — tests assert agent-deepweb attached the right thing. Canonical values are exported constants in `internal/mockdeep/server.go`; reference them symbolically.

Routes: `/whoami` (Bearer), `/basic` (Basic), `/api-key` (X-API-Key), `/login` (form → Set-Cookie + issued token + ui-state cookies), `/session` (Cookie), `/token-protected` (Bearer-from-login), `/graphql`, plus `/status/<code>`, `/slow?ms=`, `/large?bytes=`, `/echo`, `/headers`, `/redirect?to=`. Run `mockdeep -help` for the map or `GET /` on a running instance.

For unit-style integration tests, use `httptest.NewServer(mockdeep.New())` rather than spawning the binary — see `internal/api/do_test.go` for the pattern.

---

## Testing

Run `go test ./...` — the suite is currently ~30 tests across:

- `internal/api/` — redaction (headers/body/literal-echo), HTTPS enforcement, User-Agent precedence, end-to-end `Do` via httptest (ok / 404 / scheme refusal / token echo / expired session / cookie harvesting / truncation / JSON POST)
- `internal/audit/` — append/tail, disabled env, summarize
- `internal/cli/shared/` — `IsAgentMode`, `RefuseInAgentMode`, `ResolveAuth` (8 cases), helper primitives
- `internal/cli/login/` — `extractJSONToken`, `computeExpiry`, `buildLoginBody`
- `internal/credential/` — host/port/path matching (17 cases), cookie classification, mark-sensitivity
- `internal/template/` — validation (required/defaults/unknown/enum/types), substitution (URL escape, type-preserving JSON, lint)

Coverage of safety-critical paths (api: 76%, shared: 65%, audit: 74%, template: 48%, login: 25%, credential: 18%). The lower credential/login numbers reflect untested CLI registration code, not safety logic.

---

## Adding a subcommand

1. Pick or create the right file under `internal/cli/<verb>/`. For a new file, follow the `creds/` pattern (one file per subcommand, shared helpers in the package's base file).
2. If the command is agent-safe (read-only), write `RunE: func(cmd *cobra.Command, args []string) error { … }` directly.
3. If it's human-only (mutates or reveals), wrap it: `RunE: shared.HumanOnlyRunE("verb subverb", func(cmd, args) error { … })`.
4. In the RunE body, use:
   - `shared.Fail(err)` to write a classified error to stderr and return it. Never `output.WriteError(os.Stderr, err); return err` by hand.
   - `credential.WrapNotFound(err, name)` / `template.WrapNotFound(err, name)` for the common not-found pattern.
   - `shared.ResolveLimits(flagTimeout, flagMaxBytes, globals)` for request time/size caps.
   - `shared.SplitHeader(s)` / `shared.SplitKV(s, flagLabel)` for header and `key=value` parsing.
5. Register the command in the package's `Register()` function, which is called from `internal/cli/root.go`.
6. Add a test — either a pure unit test in the package, or an integration test using mockdeep.

---

## Adding an auth type

1. Add a constant to `internal/credential/credential.go` (`AuthX = "x"`) and any type-specific fields to `Secrets`.
2. Implement a `buildXSecrets(o *addOpts) (credential.Secrets, error)` in `internal/cli/creds/add.go` and register it in the `secretsBuilders` map.
3. Add a branch in `internal/api/auth.go`'s `ApplyAuth` for attaching the auth to requests.
4. If the type needs the redactor to mask its secret bytes, extend `RedactSecretEcho` in `internal/api/redact.go`.
5. Add an endpoint to `internal/mockdeep/server.go` and a test case in `internal/api/do_test.go`.

---

## Env vars

- `AGENT_DEEPWEB_MODE=agent` — LLM-safety mode (human-only commands refused, redaction forced)
- `AGENT_DEEPWEB_AUTH` — default credential for `--auth`
- `AGENT_DEEPWEB_CONFIG_DIR` — override `~/.config/agent-deepweb` (use in tests!)
- `AGENT_DEEPWEB_TIMEOUT` — default request timeout (ms)
- `AGENT_DEEPWEB_USER_AGENT` — fallback User-Agent
- `AGENT_DEEPWEB_AUDIT=off` — disable audit log writes

---

## Releasing

Uses goreleaser. `go install github.com/shhac/agent-deepweb/cmd/agent-deepweb@latest` for source install.

---

## Deferred work

The v1 design doc's *Future* section is the authoritative list. Currently:

- **Browser-assisted login** (chromedp/playwright). The form-login flow covers most cases; browser login would add ~15 MB to the binary and needs external helper. Interface sketched in the design doc.
- **Response diffs / caching** for polling use cases. Audit log plus re-fetch is good enough today.
- **Per-cookie-attribute harvesting from cookiejar.Cookies()**. The stdlib jar drops Domain/Path/Expires/HttpOnly on retrieval; we harvest from `resp.Cookies()` (Set-Cookie headers) instead. Works, but means cookies set by redirect hops in the middle of a jar-driven fetch lose their attributes when persisted.
