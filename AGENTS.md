# agent-deepweb — AGENTS guide

`curl`-with-auth for AI agents. Humans register **profiles** (auth identities); LLMs invoke them by name. The LLM never sees the secret values.

This file is the entry point for an agent (human or AI) picking up the repo. Read top-to-bottom the first time; skim section links thereafter. `CLAUDE.md` is a symlink to this file — single source of truth.

---

## Where to start

1. **Read [README.md](README.md)** for the user-facing pitch: install, quick-start, command reference, security guarantees.
2. **Read [skills/agent-deepweb/SKILL.md](skills/agent-deepweb/SKILL.md)** for the LLM-facing contract and the recommended Claude Code permission allowlist.
3. **Read [design-docs/v2-profiles.md](design-docs/v2-profiles.md)** (gitignored, local-only) for v2's rationale: why we dropped the mode infrastructure, why escalation requires re-asserting the primary secret, the open questions decided. v1 design is in `v1-design.md`.
4. **Skim [Project structure](#project-structure)** below.
5. **Pick your task** — if it's:
   - a **new subcommand** → see [Adding a subcommand](#adding-a-subcommand)
   - a **new auth type** → see [Adding an auth type](#adding-an-auth-type)
   - a **bug in redaction/classification/matching** → go straight to the unit tests in the same package and add a failing case
   - **anything touching HTTP** → spin up `make mock`, set `AGENT_DEEPWEB_CONFIG_DIR=/tmp/somewhere`, drive the binary against `mockdeep` before writing production code

Before committing: `make fmt && make lint && make test`.

---

## The v2 security model in one paragraph

agent-deepweb's job is to not be a hole in the harness's sandbox. The harness (Claude Code) decides which subcommands the LLM can invoke; agent-deepweb makes sure each subcommand can't be misused. The handful of escalation paths (`profile allow / allow-path`, `profile set-default-header`, `profile set-allow-http`, `session mark-visible`) require re-supplying the profile's primary secret as a flag. We don't verify the value — we OVERWRITE the stored secret with whatever was supplied. A human who knows the secret produces a no-op overwrite; an LLM that doesn't ends up with a useless broken profile. Every request is audited.

There is no "agent mode" / "human mode" anymore. Anonymous requests must be opt-in via `--no-auth` (no silent fallthrough). Redaction is mandatory (no `--no-redact` flag). `--allow-http` is per-profile only (set with `profile set-allow-http`).

---

## Project structure

```
cmd/
  agent-deepweb/main.go             Entry point — version stamped via ldflags
  mockdeep/main.go                  Demo server used by e2e tests
internal/
  api/                              HTTP transport layer
    client.go                       Request/Response/ClientOptions + Do() orchestrator + audit defer
    request.go                      buildHTTPRequest, resolveUserAgent, viewPersisted
    scheme.go                       enforceScheme, isLoopback
    classify.go                     classifyTransport, classifyHTTP → fixable_by
    auth.go                         ApplyAuth per credential type
    redact.go                       Header / JSON body / literal-byte echo redactors
  audit/log.go                      Append-only JSONL request log
  cli/                              Cobra command tree
    root.go                         Global flags + subcommand registration
    usage.go                        Top-level llm-help card
    shared/                         Helpers used by every subcommand
      shared.go                     GlobalFlags, ResolveAuth (URL allowlist + ambiguity)
      helpers.go                    Fail*, PrintOK, First*, SplitHeader, SplitKV, ResolveLimits
      secret_assert.go              SecretAssert + escalateOverwrite — the v2 escalation gate
    fetch/                          `fetch` — curl-with-auth
    graphql/                        `graphql` — POST + error classification
    profile/                        `profile {list,show,test,add,remove,allow,…}` — split per file
      profile.go                    Register + list + show
      test.go / add.go / remove.go  One file per subcommand
      domains.go                    allow/disallow/allow-path/disallow-path
      config.go                     set-health/set-default-header/set-allow-http/set-user-agent
    login/                          `login` + `session …`
      login.go                      Register wiring
      session.go                    status / show / clear / set-expires / mark-*
      form.go                       doLogin + helpers (the form-login engine)
    tpl/                            `tpl {list,show,run,import,remove}`
    audit/                          `audit tail / summary`
  config/config.go                  App config I/O + DefaultTimeoutMS / DefaultMaxBytes constants
  credential/                       Profile storage (legacy package name; see note below)
    credential.go                   Types (Credential, Secrets, Resolved, indexEntry)
    store.go                        Store/Remove + index+secrets file I/O
    query.go                        List / GetMetadata / Resolve
    mutate.go                       Set*(name, …) setters
    match.go                        MatchesURL — host[:port] + path allowlist
    cookie.go                       PersistedCookie + sensitivity classification
    session.go                      Session (cookies + token + expiry) + jar helpers
    keychain.go                     macOS Keychain adapter
    notfound.go                     WrapNotFound / ClassifyLookupErr
  errors/errors.go                  APIError{Message, Hint, FixableBy, Cause}
  mockdeep/server.go                Mock HTTP server (one handler per auth mode)
  output/                           LLM-facing output
    output.go                       PrintJSON, WriteError, format parsing
    envelope.go                     BuildHTTPEnvelope, RenderBody
  template/                         Request templates (highest-safety mode)
    template.go                     Template / ParamSpec types + Store/Get/List/Remove/Import
    validate.go                     Type coercion, Required/Default/Enum
    substitute.go                   {{param}} rendering
    notfound.go                     WrapNotFound / ClassifyLookupErr
design-docs/v1-design.md            v1 (mode-gated) rationale
design-docs/v2-profiles.md          v2 (profiles + password-protect) rationale and decisions
skills/agent-deepweb/SKILL.md       Claude Code skill definition + permission allowlist
```

**Naming note**: the user-facing CLI verb is `profile`, the Go subpackage is `internal/cli/profile`, but the storage package is still `internal/credential`. The storage layer doesn't expose user-facing strings, so the package name is implementation detail; renaming would touch ~50 files for no functional gain.

---

## Command surface

| Verb | Notes |
|------|-------|
| `fetch` | curl-with-auth. `--no-auth` for anonymous. |
| `graphql` | POST with classified GraphQL error envelope. |
| `tpl` | Parameterised request templates. `tpl run` is the LLM-facing verb. |
| `profile` | CRUD for profiles. Escalation commands require primary secret re-assertion. |
| `login` / `session` | Form-login flow + session introspection. |
| `audit` | Inspect the append-only request log. |
| `llm-help` | Per-verb reference cards. |

The harness (Claude Code) decides which of these the LLM can run. SKILL.md ships the recommended allowlist.

---

## Key design decisions

- **Profiles as the unit.** A profile bundles secret material, host[:port]+path allowlist, default headers, User-Agent override, and (for form-auth) a derived session. CLI verb is `profile`. Storage package stayed named `credential` (impl detail).
- **No mode infrastructure.** `AGENT_DEEPWEB_MODE` is gone. Anonymous requests are opt-in via `--no-auth`; redaction is unconditional; `--allow-http` is per-profile only.
- **Primary-secret re-assertion on escalation.** Commands that widen scope (`profile allow / allow-path`) or change defaults that affect outbound requests (`set-default-header / set-allow-http`) or un-mask cookies (`session mark-visible`) require the profile's primary secret as flag(s). We OVERWRITE the stored secret with what's supplied — wrong value silently breaks the profile.
- **URL allowlist per profile.** `host`, `host:port`, `*.wildcard`, `*.wildcard:port`, plus optional path patterns (exact, `/prefix/*`, `path.Match` glob).
- **Anonymous requests refused unless explicit.** `ResolveAuth` errors when no profile matches the URL — the caller must pass `--no-auth` to opt into anonymous. Closes the v1 hole where forgetting to register a profile turned agent-deepweb into a generic outbound HTTP client.
- **https-only by default.** `http://` is refused for auth-attaching profiles unless the host is loopback or the profile has `allow_http: true` (set with `profile set-allow-http <name> true --token ...`).
- **Cookie jar + per-cookie sensitivity classification.** Form auth uses `net/http/cookiejar` with the public-suffix list. Each persisted cookie carries a `Sensitive` bool (HttpOnly OR auth-name regex). `session show` reveals visible cookies with values, sensitive cookies as `<redacted>`.
- **Form-login.** `agent-deepweb login <name>` POSTs form/JSON credentials, harvests Set-Cookie, optionally extracts a Bearer token from the JSON body at `--token-path`, computes expiry as `min(session-ttl, latest-cookie-expiry, 24h)`. The login response body is never returned to the caller.
- **Redaction is unconditional.** Response headers + JSON body fields matching secret patterns are `<redacted>`; byte-level substitution masks literal secret values anywhere they appear.
- **Per-profile default headers.** Non-secret headers applied to every request (e.g., `Accept: application/vnd.api+json`).
- **Keychain on macOS** (service `app.paulie.agent-deepweb`); 0600 file fallback elsewhere. Sessions always on disk at `sessions/<name>.json`.
- **Fixable-by hints everywhere.** Every error is `{error, hint, fixable_by}` with `agent | human | retry`.
- **Request templates.** Fixed method, URL with `{{param}}` placeholders, optional query/headers/body_template, profile binding, typed parameter schema. LLM runs `tpl run <name> --param k=v`; values are coerced and validated *before* any HTTP request. Body substitution is type-preserving (an `int` param becomes a JSON number, not a string).
- **Audit log.** Every fetch/graphql/tpl-run writes one JSONL entry to `~/.config/agent-deepweb/audit.log` with method, host, path, profile name, template name, status, bytes, duration, and `outcome`+`fixable_by` on errors. Never includes bodies, headers, or query strings. Opt out via `AGENT_DEEPWEB_AUDIT=off`.
- **User-Agent precedence.** per-request `--user-agent` > profile's UA > `User-Agent` request header > `AGENT_DEEPWEB_USER_AGENT` env > `agent-deepweb/<version>` (curl-style).
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
make dev ARGS="fetch https://httpbin.org/get --no-auth"
make mock           # Run mockdeep on :8765
```

**Linter config** lives at `.golangci.yml` (v2 schema, default linters, narrow `errcheck` exclusions for `cobra.MarkFlagRequired` + `_test.go` files). Currently zero issues.

---

## Mock server (`mockdeep`)

`cmd/mockdeep` is a dumb sibling binary that drives e2e tests. Each auth style has its own endpoint accepting one hardcoded credential. Canonical values are exported constants in `internal/mockdeep/server.go`.

Routes: `/whoami` (Bearer), `/basic`, `/api-key` (X-API-Key), `/login` (form → Set-Cookie + issued token), `/session`, `/token-protected`, `/graphql`, `/status/<code>`, `/slow`, `/large`, `/echo`, `/headers`, `/redirect`. Run `mockdeep -help` for the map; `mockdeep -creds` for the canonical credential values.

For unit-style integration tests, use `httptest.NewServer(mockdeep.New())` rather than spawning the binary — see `internal/api/do_test.go` for the pattern.

---

## Adding a subcommand

1. Pick or create the right file under `internal/cli/<verb>/`.
2. Write `RunE: func(cmd *cobra.Command, args []string) error { … }` — no special wrapping.
3. In the body, use:
   - `shared.Fail(err)` to emit a classified error to stderr and return it. Never `output.WriteError(...) ; return err` by hand.
   - `shared.FailHuman(err)` / `shared.FailAgent(err)` for the common wrap-and-fail one-liners.
   - `shared.PrintOK(map[string]any{...})` for the canonical `{"status":"ok",…}` success envelope.
   - `credential.ClassifyLookupErr(err, name)` / `template.ClassifyLookupErr(err, name)` for the not-found classification.
   - `shared.ResolveLimits(flagTimeout, flagMaxBytes, globals)` for request time/size caps.
   - `shared.SplitHeader(s)` / `shared.SplitKV(s, flagLabel)` for header and `key=value` parsing.
4. **If the subcommand is an escalation** (widens scope, reveals stored secret, changes outbound auth shape):
   - Bind the primary-secret flags via `shared.BindSecretAssertFlags(cmd, &assert)`
   - Call `shared.ApplySecretAssert(c, &assert)` before the mutation
   - Document why in the function comment
5. Register the command in the package's `Register()` function, called from `internal/cli/root.go`.
6. Add a test — pure unit if possible, else integration via mockdeep.

---

## Adding an auth type

1. Add a constant to `internal/credential/credential.go` (`AuthX = "x"`) and any type-specific fields to `Secrets`.
2. Implement a `buildXSecrets(o *addOpts) (credential.Secrets, error)` in `internal/cli/profile/add.go` and register it in the `secretsBuilders` map.
3. Add a branch in `internal/api/auth.go`'s `ApplyAuth` for attaching the auth to requests.
4. Add a branch to `internal/cli/shared/secret_assert.go`'s `BuildSecretsForAssert` so escalation commands can re-assert the primary secret.
5. If the type needs the redactor to mask its secret bytes, extend `RedactSecretEcho` in `internal/api/redact.go`.
6. Add an endpoint to `internal/mockdeep/server.go` and a test case in `internal/api/do_test.go`.

---

## Env vars

- `AGENT_DEEPWEB_AUTH` — default profile name for `--auth`
- `AGENT_DEEPWEB_CONFIG_DIR` — override `~/.config/agent-deepweb` (use in tests!)
- `AGENT_DEEPWEB_TIMEOUT` — default request timeout (ms)
- `AGENT_DEEPWEB_USER_AGENT` — fallback User-Agent
- `AGENT_DEEPWEB_AUDIT=off` — disable audit log writes

---

## Releasing

Uses goreleaser. `go install github.com/shhac/agent-deepweb/cmd/agent-deepweb@latest` for source install. See `.claude/commands/release.md` for the full release flow.

---

## Deferred work

- **Phase D** of the v2 design: cookie jar generalization for non-form profiles + `--profile none` as the explicit anonymous opt-in (replacing `--no-auth`) + `--cookiejar <path>` BYO-jar for LLM-authored exploration flows.
- **Browser-assisted login** (chromedp). Form-login handles most cases.
- **Native Security Framework API (CGo)** for true keychain ACLs. Park behind harness-cooperation.
- **Response diffs / caching** for polling use cases.
