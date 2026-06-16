# cofiswarm-observer

Cofiswarm component: `observer`.

- Layout: [REPO-STANDARD-LAYOUT](https://github.com/keepdevops/cofiswarmdev/blob/main/docs/REPO-STANDARD-LAYOUT.md)
- Migration: [MIGRATION-SPRINTS](https://github.com/keepdevops/cofiswarmdev/blob/main/docs/MIGRATION-SPRINTS.md)

## FHS paths

| Path | Purpose |
|------|---------|
| `/etc/cofiswarm/observer/` | config |
| `/var/lib/cofiswarm/observer/` | state |
| `/var/log/cofiswarm/observer/` | logs |

## Test

```bash
./test/scripts/assert-layout.sh observer
```
