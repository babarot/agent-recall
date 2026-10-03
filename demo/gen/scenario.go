package main

import "time"

// language is the demo in one language: its sessions, the notes Claude
// writes before a tool call (fmt forms of the file or command), and what the
// stand-in claude answers when asked with a: the searches it shows, and the
// sessions it finds, by title, with why each matches.
type language struct {
	sessions         []session
	read, edit, bash []string
	grep             string
	searches         []string
	found            [][2]string
}

var languages = map[string]language{"en": english, "ja": japanese}

// session is one demo session: where it ran, what it was about, and the
// tools it used. The conversation is padded with tool steps up to msgs
// messages so the counts look like real sessions.
type session struct {
	dir      string // under the demo home
	branch   string
	title    string
	ago      time.Duration // since its last message
	msgs     int
	prompt   string
	reply    string
	files    []string // edited, relative to dir
	commands []string // run with Bash
	talk     []string // more of the conversation, the user and Claude in turn
	last     string   // the last thing the user said
	answer   string   // Claude's last reply
}

const (
	api   = "src/github.com/acme/api"
	web   = "src/github.com/acme/web"
	infra = "src/github.com/acme/infra"
	cli   = "src/github.com/acme/cli"
	dots  = "dotfiles"
	notes = "notes" // not a git repository

	refreshWT = api + "/.claude/worktrees/fix-token-refresh"
	tracingWT = api + "/.claude/worktrees/add-tracing"
	removedWT = api + "/.claude/worktrees/orders-schema" // removed since: shown struck through
	webWT     = ".herdr/worktrees/web/worktree-quiet-meadow-4f2a"
)

// worktrees are the linked worktrees the generator creates, by repository.
var worktrees = map[string][]struct{ path, branch string }{
	api: {{refreshWT, "fix-token-refresh"}, {tracingWT, "add-tracing"}},
	web: {{webWT, "worktree/quiet-meadow-4f2a"}},
}

const (
	h = time.Hour
	d = 24 * time.Hour
)

var english = language{
	read:     []string{"Let me look at %s first.", "Checking how %s handles this.", "Reading %s."},
	edit:     []string{"Updating %s.", "Making the change in %s.", "Now the edit in %s."},
	bash:     []string{"Running `%s`.", "Let me check with `%s`.", "Now `%s` to confirm."},
	grep:     "Searching for it.",
	searches: []string{"logged out several requests at once", "token refresh concurrent"},
	found: [][2]string{
		{"Fix the token refresh race on concurrent requests", "Found that concurrent refreshes revoked each other and fixed it with singleflight."},
		{"Write tests for token refresh under load", "Added a 50-goroutine test proving only one refresh reaches the provider."},
	},
	sessions: []session{
		{
			dir: refreshWT, branch: "fix-token-refresh", ago: 12 * time.Minute, msgs: 96,
			title: "Write tests for token refresh under load",
			talk: []string{
				"Should the fake provider sleep to widen the race window?",
				"Yes, 20 ms per call. Without it the goroutines rarely overlap and the test would pass even without the fix.",
				"Check that it fails on the old code.",
				"Reverted refresh.go to main: the test fails with 50 provider calls instead of 1. With the fix it passes.",
			},
			prompt:   "Add a test that refreshes the same token from 50 goroutines and checks that only one refresh hits the identity provider.",
			reply:    "I'll add a table-driven test in internal/auth with a fake provider that counts calls, then run it with the race detector.",
			files:    []string{"internal/auth/refresh_test.go", "internal/auth/fake_provider_test.go"},
			commands: []string{"go test -race -run TestRefreshConcurrent ./internal/auth/", "go test -race ./..."},
			last:     "Run it 100 times to make sure it is not flaky.",
			answer:   "100 runs with -race -count=100 all pass. The provider is called exactly once per expired token.",
		},
		{
			dir: refreshWT, branch: "fix-token-refresh", ago: 50 * time.Minute, msgs: 248,
			title: "Fix the token refresh race on concurrent requests",
			talk: []string{
				"How do you know the provider revokes the refresh token?",
				"Its token endpoint returns invalid_grant for the second request in the logs, 40 ms after the first succeeded, with the same refresh token.",
				"Can we reproduce it locally?",
				"Yes. With the dev provider and an access token that expires after 5 seconds:\n\n- 1 request at a time: never logged out\n- 10 requests at once: logged out 9 times out of 10\n\nThat matches the reports, which all come from the dashboard, the page that loads 8 widgets in parallel.",
				"Use singleflight so concurrent refreshes share one call.",
				"Done. Refreshes now go through a singleflight group keyed by the refresh token:\n\n    tok, err, _ := r.group.Do(rt, func() (any, error) {\n        return r.provider.Refresh(ctx, rt)\n    })\n\nThe first request refreshes; the others wait for it and reuse the new access token. go test -race ./internal/auth/... passes.",
			},
			prompt:   "Users get logged out randomly when the app sends several requests at once right after the access token expires. Find out why.",
			reply:    "Found it. Each request that sees an expired access token starts its own refresh:\n\n- request A sends the refresh token and gets a new pair\n- request B sends the same refresh token 40 ms later\n- the provider takes the reuse for a stolen token, revokes the family, and B logs the user out\n\nSo we refresh once per request where we should refresh once per user.",
			files:    []string{"internal/auth/refresh.go", "internal/auth/middleware.go", "internal/auth/store.go"},
			commands: []string{"go test ./internal/auth/...", "rg -n 'Refresh\\(' internal", "go vet ./..."},
			last:     "What about two API instances refreshing at once?",
			answer:   "singleflight only covers one process. I'll also take a short Redis lock per user so a second instance waits and reads the new token.",
		},
		{
			dir: api, branch: "main", ago: 3 * h, msgs: 182,
			title: "Add rate limiting to the public endpoints",
			talk: []string{
				"Where does the bucket state live? We run four instances.",
				"In Redis, with a Lua script that refills and takes a token atomically, so the four instances share one limit per client.",
				"What happens if Redis is down?",
				"The middleware fails open and logs a warning, so an outage of Redis does not take the API down with it.",
			},
			prompt:   "Add per-client rate limiting to the public API: 100 requests a minute, with a Retry-After header when the limit is hit.",
			reply:    "I'll add a token bucket middleware keyed by API key, falling back to the client IP, and wire it in front of the /v1 routes.",
			files:    []string{"internal/http/ratelimit.go", "internal/http/router.go", "internal/http/ratelimit_test.go"},
			commands: []string{"go test ./internal/http/...", "hey -n 300 -c 20 http://localhost:8080/v1/orders"},
			last:     "Make the limit configurable per plan.",
			answer:   "The limit now comes from the plan in the API key's metadata, with 100/min as the default.",
		},
		{
			dir: api, branch: "main", ago: 27 * h, msgs: 74,
			title:    "Shorten the session cookie lifetime",
			prompt:   "Security asked us to cut the session cookie lifetime from 30 days to 7. Where is it set?",
			reply:    "In internal/auth/cookie.go, as MaxAge on the session cookie. The remember-me option sets it separately in the login handler.",
			files:    []string{"internal/auth/cookie.go", "internal/http/login.go"},
			commands: []string{"go test ./internal/auth/...", "rg -n MaxAge internal"},
			talk: []string{
				"Should remember-me stay at 30 days?",
				"Security's note says 7 days for every session, remember-me included, so both change.",
				"Will people with an older cookie be signed out right away?",
				"No. Existing cookies keep the expiry they were issued with; the new lifetime applies from the next sign-in.",
			},
			last:   "Add a note to the changelog.",
			answer: "Added under Changed: session cookies now last 7 days, down from 30.",
		},
		{
			dir: api, branch: "main", ago: 4*d + 2*h, msgs: 226,
			title:    "Debug users getting 401s after the deploy",
			prompt:   "Since this morning's deploy some users get 401s and are signed out of the app mid-session. Can you find out why?",
			reply:    "The 401s come from the token check in the auth middleware, and only on requests served by api-3. I'll compare it with the other nodes.",
			files:    []string{"internal/auth/jwt.go", "internal/auth/jwt_test.go", "deploy/chrony.conf"},
			commands: []string{"kubectl logs deploy/api --since=2h | rg ' 401 '", "kubectl exec api-3 -- date -u", "go test ./internal/auth/..."},
			talk: []string{
				"Could it be the session store? We moved it to the new Redis cluster in the same deploy.",
				"I checked that first. Session lookups succeed on every node and the keys are all there.\n\nThe 401s come from somewhere else: every one of them is a token validation error in the middleware, not a missing session.",
				"What does the validation error say?",
				"token used before issued: the iat claim is about 90 seconds in the future when api-3 checks it.",
				"So tokens from the future?",
				"Not quite: api-3's clock is behind.\n\n- api-1, api-2 and the other nodes: within 5 ms of each other\n- api-3: 91 seconds behind\n\nA token issued by any other node looks, to api-3, like it was issued in the future, and the JWT library rejects it. The user gets a 401 and the app signs them out.",
				"Why only since the deploy?",
				"The deploy moved api-3 to a new node pool whose image has chrony disabled, so its clock has been drifting since it started.",
				"Is it only api-3?",
				"Yes. The other five pods run on the old pool and are within 5 ms of each other.",
				"How do we stop this from happening again?",
				"Two things:\n\n1. Enable chrony in the new node image, so the clocks stay in sync.\n2. Allow 60 seconds of leeway on iat and nbf, so a small drift never signs anyone out again.\n\nThe leeway is a one-line change in jwt.go; the image needs the platform team.",
			},
			last:   "Ship the leeway now and open an issue for the image.",
			answer: "The leeway is deployed and the 401s have stopped; the node image issue is #318.",
		},
		{
			dir: web, branch: "main", ago: 5 * h, msgs: 118,
			title: "Fix the login form losing focus on error",
			talk: []string{
				"Why does the key change?",
				"It is built from the error message plus a timestamp so the banner animates again on each attempt; the timestamp is what remounts the form.",
				"Keep the animation.",
				"The banner now replays its animation through a CSS class toggled on each attempt, without touching the key.",
			},
			prompt:   "When the password is wrong, the login form re-renders and the cursor leaves the password field. Fix it.",
			reply:    "The error banner is rendered above the form with a key that changes on every attempt, which remounts the form. I'll keep the key stable.",
			files:    []string{"src/pages/Login.tsx", "src/components/ErrorBanner.tsx"},
			commands: []string{"npm test -- Login", "npm run lint"},
			last:     "Also announce the error to screen readers.",
			answer:   "The banner now has role=\"alert\", and focus stays in the password field with its text selected.",
		},
		{
			dir: api, branch: "main", ago: 26 * h, msgs: 316,
			title: "Investigate the slow /orders query",
			talk: []string{
				"Is the index safe to create on production while it serves traffic?",
				"With CREATE INDEX CONCURRENTLY, yes. It takes longer and cannot run inside a transaction, so the migration opts out of one.",
				"How big will it be?",
				"About 1.2 GB for 31 million rows, roughly a quarter of the table.",
			},
			prompt:   "GET /v1/orders takes 4 seconds for large customers. Find out where the time goes.",
			reply:    "EXPLAIN ANALYZE shows a sequential scan on orders filtered by customer_id and sorted by created_at. A composite index should fix it.",
			files:    []string{"migrations/0042_orders_customer_created_idx.sql", "internal/store/orders.go"},
			commands: []string{"psql -c 'EXPLAIN ANALYZE SELECT ...'", "go test ./internal/store/...", "make migrate"},
			last:     "What is the p95 now?",
			answer:   "p95 for the largest customer went from 4.1 s to 38 ms with the index on (customer_id, created_at DESC).",
		},
		{
			dir: webWT, branch: "worktree/quiet-meadow-4f2a", ago: 30 * h, msgs: 262,
			title: "Redesign the settings page with tabs",
			talk: []string{
				"Should each tab load its own data?",
				"Yes, lazily on first open, so the page no longer fetches billing and API keys for people who only change their name.",
				"Keyboard navigation?",
				"Arrow keys move between tabs and Home and End jump to the first and last, following the ARIA tabs pattern.",
			},
			prompt:   "Split the settings page into tabs: Profile, Notifications, Billing and API keys. Keep the URLs bookmarkable.",
			reply:    "I'll add a Tabs component driven by the URL hash, move each section into its own component, and keep the old anchors redirecting.",
			files:    []string{"src/pages/Settings.tsx", "src/components/Tabs.tsx", "src/pages/settings/Billing.tsx", "src/pages/settings/ApiKeys.tsx"},
			commands: []string{"npm test -- Settings", "npx playwright test settings"},
			last:     "The Billing tab flickers on load.",
			answer:   "It was fetching the plan twice; the tab now reads it from the shared query cache and renders once.",
		},
		{
			dir: infra, branch: "add-cdn", ago: 2 * d, msgs: 204,
			title: "Put the web app behind a CDN",
			talk: []string{
				"What about cookies on /api?",
				"/api/* forwards all cookies and the Authorization header and is never cached; /assets/* forwards none.",
				"How long does the first apply take?",
				"About 18 minutes, most of it for CloudFront to deploy the distribution.",
			},
			prompt:   "Serve the web app's static assets from a CDN and keep the API on the origin.",
			reply:    "I'll add a CloudFront distribution with two origins, cache /assets/* for a year, and pass /api/* through uncached.",
			files:    []string{"modules/cdn/main.tf", "modules/cdn/variables.tf", "envs/production/cdn.tf"},
			commands: []string{"terraform fmt -recursive", "terraform plan -out plan.out", "terraform apply plan.out"},
			last:     "Invalidate the cache on deploy.",
			answer:   "The deploy workflow now runs an invalidation for /index.html after uploading the assets.",
		},
		{
			dir: tracingWT, branch: "add-tracing", ago: 2*d + 4*h, msgs: 151,
			title: "Add OpenTelemetry tracing to the HTTP handlers",
			talk: []string{
				"Will this slow requests down?",
				"With the batch span processor the overhead is about 40 microseconds per request in the benchmark, under 1% of the median latency.",
				"Sample it in production.",
				"Production now samples 10% of traces with a parent-based sampler, so a trace started upstream is always kept.",
			},
			prompt:   "Add OpenTelemetry tracing to every HTTP handler and the database calls, exported over OTLP.",
			reply:    "I'll wrap the router with otelhttp, add spans around the store methods, and read the exporter endpoint from OTEL_EXPORTER_OTLP_ENDPOINT.",
			files:    []string{"internal/telemetry/tracing.go", "internal/http/router.go", "internal/store/orders.go", "cmd/api/main.go"},
			commands: []string{"go get go.opentelemetry.io/otel@latest", "go test ./...", "docker compose up -d jaeger"},
			last:     "Show me a trace for POST /v1/orders.",
			answer:   "The trace has the handler span, the validation span and two database spans; the insert takes most of the 22 ms.",
		},
		{
			dir: api, branch: "main", ago: 3 * d, msgs: 58,
			title: "Review PR #142: pagination for /users",
			talk: []string{
				"Anything else in the PR?",
				"The page size is not capped, so ?limit=100000 loads every user. I'd cap it at 100.",
				"Is the test coverage enough?",
				"It tests the first and second page but not the last one or an empty result; I'd add both.",
			},
			prompt:   "Review PR #142, which adds cursor pagination to /v1/users.",
			reply:    "The cursor encodes the last ID but not the sort key, so pages skip users when two share a created_at. I'd encode both.",
			commands: []string{"gh pr checkout 142", "gh pr diff 142", "go test ./internal/http/..."},
			last:     "Leave that as a review comment.",
			answer:   "Posted the comment on internal/http/users.go with a suggested fix for the cursor.",
		},
		{
			dir: cli, branch: "main", ago: 3*d + 6*h, msgs: 41,
			title: "Release v2.3.0 and update the changelog",
			talk: []string{
				"Which PRs are breaking?",
				"None. #87 renames a flag, but the old name still works with a deprecation warning.",
				"Mention the deprecation.",
				"Added a Deprecated section noting that --out becomes --output and the old name goes away in v3.",
			},
			prompt:   "Cut the v2.3.0 release and write the changelog from the merged PRs since v2.2.0.",
			reply:    "There are 9 merged PRs since v2.2.0: 4 features and 5 fixes. I'll group them and tag the release.",
			files:    []string{"CHANGELOG.md"},
			commands: []string{"gh pr list --state merged --base main --search 'merged:>2026-09-01'", "git tag v2.3.0", "git push origin v2.3.0"},
			last:     "Check the release workflow.",
			answer:   "The release workflow passed and the binaries for all four platforms are attached to v2.3.0.",
		},
		{
			dir: web, branch: "main", ago: 4 * d, msgs: 497,
			title: "Upgrade to React 19 and fix the type errors",
			talk: []string{
				"Which of our dependencies do not support React 19 yet?",
				"Only the date picker; its latest release lists React 19 in peer dependencies, so bumping it fixes the warning.",
				"Any runtime warnings?",
				"Two components still use forwardRef, which works but is no longer needed; I converted them to plain ref props.",
			},
			prompt:   "Upgrade the web app to React 19 and fix whatever breaks.",
			reply:    "The upgrade leaves 63 type errors, mostly from the removed implicit children prop and the new ref handling.",
			files:    []string{"package.json", "src/components/Modal.tsx", "src/components/Tooltip.tsx", "src/hooks/useForm.ts"},
			commands: []string{"npm install react@19 react-dom@19", "npx tsc --noEmit", "npm test"},
			last:     "Are the tests green?",
			answer:   "All 412 tests pass and tsc reports no errors.",
		},
		{
			dir: infra, branch: "main", ago: 5 * d, msgs: 72,
			title: "Rotate the database credentials in Vault",
			talk: []string{
				"How long do the pods keep the old credentials?",
				"Until they restart: the app reads the credentials at startup, so the rollout is what moves them to the new role.",
				"Can we make the next rotation automatic?",
				"Yes, with the Vault agent sidecar renewing the lease and the app reloading its pool on change; I'll write it up.",
			},
			prompt:   "Rotate the production database credentials and make sure nothing still uses the old ones.",
			reply:    "I'll create a new role in Vault's database engine, roll the API pods, and revoke the old lease once the connections drain.",
			commands: []string{"vault write database/rotate-role/api", "kubectl rollout restart deploy/api", "vault lease revoke -prefix database/creds/api-old"},
			last:     "Any errors after the rollout?",
			answer:   "No connection errors in the last 30 minutes, and the old role has no open connections.",
		},
		{
			dir: removedWT, branch: "orders-schema", ago: 6 * d, msgs: 408,
			title: "Migrate the orders table to the new schema",
			talk: []string{
				"How do we know the dual-write is correct?",
				"A nightly job compares order totals between the old columns and the summed line items and alerts on any difference.",
				"How long will the backfill take?",
				"At 5000 orders a batch with a 100 ms pause, about 3 hours for 2.4 million orders, without visible load on the primary.",
			},
			prompt:   "Move orders to the new schema with line items in their own table, without downtime.",
			reply:    "I'll do it in three steps: add the new table and dual-write, backfill in batches, then switch reads and drop the old columns.",
			files:    []string{"migrations/0039_line_items.sql", "migrations/0040_backfill_line_items.sql", "internal/store/orders.go"},
			commands: []string{"make migrate", "go run ./cmd/backfill --batch 5000", "go test ./internal/store/..."},
			last:     "Is the backfill done?",
			answer:   "The backfill finished: 2.4 million orders, and the checksums of old and new totals match.",
		},
		{
			dir: cli, branch: "main", ago: 7 * d, msgs: 109,
			title: "Add shell completion to the CLI",
			talk: []string{
				"Project names come from the API. Is that too slow for completion?",
				"It would be, so completion reads the local cache and refreshes it in the background when it is older than an hour.",
				"And with no cache yet?",
				"It completes the subcommands and flags and skips the project names until the first refresh.",
			},
			prompt:   "Add shell completion for bash, zsh and fish, including completion of project names.",
			reply:    "Cobra generates the scripts; I'll add a ValidArgsFunction that lists projects from the local cache.",
			files:    []string{"cmd/completion.go", "cmd/projects.go", "README.md"},
			commands: []string{"go build ./...", "source <(./acme completion zsh)"},
			last:     "Document it.",
			answer:   "The README has a Shell completion section with the setup line for each shell.",
		},
		{
			dir: dots, branch: "main", ago: 8 * d, msgs: 52,
			title: "Configure Ghostty keybindings",
			talk: []string{
				"Does cmd+d conflict with anything?",
				"Only the bookmark shortcut in some browsers; inside Ghostty it is free.",
				"Can I resize splits from the keyboard?",
				"Added cmd+ctrl+arrow to resize the focused split by 10 cells.",
			},
			prompt:   "Set up Ghostty so cmd+d splits right, cmd+shift+d splits down, and cmd+[ and cmd+] move between splits.",
			reply:    "I'll add the keybinds to ghostty/config and reload it.",
			files:    []string{"ghostty/config"},
			commands: []string{"ghostty +list-keybinds | rg split"},
			last:     "Also make new splits keep the working directory.",
			answer:   "Added window-inherit-working-directory = true.",
		},
		{
			dir: web, branch: "main", ago: 9 * d, msgs: 138,
			title: "Add a dark mode toggle",
			talk: []string{
				"Does it need three states?",
				"Yes: light, dark and system. System stays the default and is what clearing the choice returns to.",
				"Check the contrast.",
				"All text tokens meet 4.5:1 in both themes except the muted label in dark mode; I lightened it to 4.8:1.",
			},
			prompt:   "Add a dark mode toggle to the header that follows the system setting until the user picks one.",
			reply:    "I'll keep the choice in localStorage, default to prefers-color-scheme, and switch a data-theme attribute on <html>.",
			files:    []string{"src/components/ThemeToggle.tsx", "src/styles/tokens.css", "src/components/Header.tsx"},
			commands: []string{"npm test -- ThemeToggle"},
			last:     "There is a white flash on load in dark mode.",
			answer:   "A small inline script in index.html now sets data-theme before the first paint.",
		},
		{
			dir: infra, branch: "main", ago: 12 * d, msgs: 377,
			title: "Set up the staging environment with Terraform",
			talk: []string{
				"Should staging share the production VPC?",
				"No. A separate VPC keeps staging from reaching production by mistake, and peering can be added if it ever needs to.",
				"Where does the state go?",
				"Its own key in the same S3 bucket, with the DynamoDB lock table shared.",
			},
			prompt:   "Create a staging environment that mirrors production at a smaller size.",
			reply:    "I'll add envs/staging reusing the same modules with smaller instance sizes, its own state, and a separate VPC.",
			files:    []string{"envs/staging/main.tf", "envs/staging/terraform.tfvars", "envs/staging/backend.tf"},
			commands: []string{"terraform init", "terraform plan", "terraform apply"},
			last:     "How much will it cost?",
			answer:   "About $410 a month, mostly the database instance; scheduling it off at night would halve that.",
		},
		{
			dir: dots, branch: "main", ago: 15 * d, msgs: 88,
			title: "Speed up zsh startup",
			talk: []string{
				"Is compinit safe to cache?",
				"Yes, if the dump is rebuilt once a day; the check costs 1 ms instead of 300.",
				"What else is slow?",
				"The prompt's git status in large repositories; I switched it to the async mode.",
			},
			prompt:   "My zsh takes 900 ms to start. Make it faster.",
			reply:    "zprof shows most of the time in nvm and compinit. I'll lazy-load nvm and cache compinit's dump.",
			files:    []string{"zsh/.zshrc", "zsh/lazy.zsh"},
			commands: []string{"zsh -i -c exit", "hyperfine 'zsh -i -c exit'"},
			last:     "What is it now?",
			answer:   "Startup is down to 74 ms on average over 20 runs.",
		},
		{
			dir: notes, ago: 20 * d, msgs: 76,
			title: "Draft the Q3 incident review",
			talk: []string{
				"How long should it be?",
				"Two pages: the shared cause first, then one paragraph per incident and the action items.",
				"Make the timeline clearer.",
				"Each incident now has a three-line timeline: detected, mitigated and resolved, with times.",
			},
			prompt: "Help me draft the Q3 incident review from these three postmortems.",
			reply:  "The three incidents share a cause: config changes that skipped staging. I'll lead with that and keep each incident to one paragraph.",
			files:  []string{"incident-review-q3.md"},
			last:   "Add action items with owners.",
			answer: "Added five action items, each with an owner and a due date, at the end.",
		},
		{
			dir: notes, ago: 41 * d, msgs: 34,
			title: "Answer a customer question about webhook retries",
			talk: []string{
				"Can they change the retry schedule?",
				"Not per endpoint today. They can replay any failed event from the dashboard or the API within 30 days.",
				"Do we tell them when an endpoint keeps failing?",
				"Yes, an email goes to the account owner after 24 hours of failures, and the endpoint is disabled after 5 days.",
			},
			prompt: "A customer asks how many times we retry a failed webhook and over how long. What do we do?",
			reply:  "We retry 8 times with exponential backoff starting at 30 seconds, so the last attempt is about 2 hours after the first, then the event goes to the dead letter queue.",
			last:   "Write a short reply I can send.",
			answer: "Here is a reply that explains the 8 retries over about 2 hours and how to replay events from the dashboard.",
		},
	}}
