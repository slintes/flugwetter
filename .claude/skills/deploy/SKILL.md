---
name: deploy
description: Build, publish and restart flugwetter on the server, then verify that what is running is what was built.
when_to_use: Use when the user asks to deploy, ship, release or roll out flugwetter, or to check what version is currently running on the server.
user-invocable: true
---

`make deploy` is `build push restart`, serial by design — restarting before the push
finishes redeploys the image that was already running. The work is in the checks either
side of it.

## Before

1. **`make test`.** Deploy runs no tests. The pre-commit hook does, so anything committed
   through it has passed, but a deploy from a dirty tree has not.
2. **`make version`** prints exactly what will be stamped into the binary and the image tag:

   ```
   commit:     413c214
   build time: 2026-08-09T11:03:40Z
   image:      quay.io/slintes/flugwetter:413c214
   ```

   A `-dirty` suffix means uncommitted changes — the tag would then name a commit that does
   not contain what is being deployed. `unknown` means git could not be read at all.
3. **Push the commit first.** The image tag is only useful if the commit it names is
   fetchable.

## Deploying

```bash
make deploy
```

Expect a podman build, two pushes (`:<commit>` and `:latest`), then an ssh to `web` that
runs `/var/server/restartFlugwetter.sh` under sudo. The script pulls `:latest`, replays
`/var/server/flugwetter.yaml` as a podman pod, and restarts the nginx in front of it. It
ends with a container id and `proxy-nginx`.

**A failed registry push is worth retrying before concluding anything.** `unauthorized` from
quay.io has come up once and succeeded on an immediate retry with no login in between. Only
if it repeats is `podman login quay.io` actually needed, and that is the user's to run.

Warnings about `StopSignal SIGQUIT failed to stop container proxy-nginx ... resorting to
SIGKILL` are normal for the nginx restart.

## Verifying

The image tag proves what was built, not what is serving. Check both:

```bash
ssh web 'sudo podman logs flugwetter-flugwetter 2>&1 | head -8'
```

The first line must carry the commit just deployed:

```
flugwetter starting  commit=413c214  built=2026-08-09T11:03:40Z  go=go1.26.5
loaded airports count=17 default=EDWN
new model run  model=icon_d2  initialized=09:00Z  available=10:24Z
fetching fresh weather data  airport=EDWN
cached weather data  airport=EDWN points=168
openAIP overlay enabled
```

Compare `commit=` against `git rev-parse --short HEAD`. `loaded airports` should match the
length of `airports.json`, and `openAIP overlay enabled` confirms the key reached the
container.

### The app is on port 8082

**Not 8080.** Port 8080 on that host is a different vhost — a WordPress site — and querying
it returns someone else's page with a 200, which looks like a successful check of a
deployment that never happened.

```bash
ssh web 'curl -s http://localhost:8082/api/config | jq -c "{airports: (.airports|length), build}"'
```

### Confirming a frontend change shipped

The frontend is embedded in the binary, so a matching `commit=` implies the assets came with
it — but that reasoning has been wrong once already, via the port mistake above. For a
visible change, fetch what is actually served:

```bash
ssh web 'curl -s http://localhost:8082/ | grep -oE "/static/styles\.css[^\"]*"'
ssh web 'curl -s "http://localhost:8082/static/styles.css?v=<hash>" | grep -A 4 "\.model-run"'
```

## After

A deploy restarts the process, which restarts the model-run poller. Every `new model run`
line in the minutes after a deploy comes from the **startup** poll, not from the 15-minute
watch loop — so a deploy is not evidence that the loop works. The watch loop only shows
itself as a `new model run` followed by `fetching fresh weather data` with no
`flugwetter starting` above it, which needs the process to survive a model cycle (D2 and EU
run every 3 h, global every 6).

## Rolling back

Images are tagged with their commit, so a rollback is re-tagging an older one as `:latest`
and restarting — `make restart` alone pulls whatever `:latest` currently points at.
