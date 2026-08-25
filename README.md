<p align="center">
  <img src="assets/roadpass-logo.svg" alt="ROADPASS" width="140">
</p>

<h1 align="center">ROADPASS</h1>

<p align="center">
  <a href="https://roadpass.sensifai.com">Live platform</a> ·
  <a href="#try-the-demo">Demo</a> ·
  <a href="https://ramp.eu/#/component/cbca2f0d-280a-4f00-8dc8-611c6eb012e8">RAMP listing</a> ·
  <a href="#deployment">Deployment</a> ·
  <a href="#examples">Examples</a>
</p>

Digital material passports for recycled plastic in asphalt roads — from sorting
plant to road section. The platform is country-agnostic: any country, region, city or
road authority can run it. Every company in the chain registers its own organisation,
records what it receives, makes, tests, releases and ships (dashboard, CSV, REST or
NGSI-LD), and gets a passport for each lot. Every record is signed with the
company's key and sealed in Merkle batches on Hyperledger Fabric; anyone holding a
passport link can verify it in the browser.

Sensifai BV · CIRCULOOS Open Call 2, Track A · DEMAND-23 / PLASMIX ROAD.

```
sorting plant ─► recycler ─► additive producer ─► asphalt plant ─► contractor ─► road owner
      │              │               │                  │               │            │
      └──────── signed records (Ed25519) · checks C1–C9 · receipts by the receiver ──┘
                                     │
           PostgreSQL (record) ─► ClickHouse (dashboards) · Redis (cache, streams, SSE)
                                     │
                    worker ─► Merkle seals ─► Fabric chaincode roadpass-proof
```

## Try the demo

The live platform at **https://roadpass.sensifai.com** has a demo workspace, *Capital Paving*:
one company that plays every step from sorting plant to road owner. It is rebuilt every day at
03:00 UTC, so anything you enter is gone the next morning.

| E-mail | Job role |
|---|---|
| `admin@capital-demo.com` | Administrator |
| `dana.intake@capital-demo.com` | Intake officer |
| `jordan.sorting@capital-demo.com` | Sorting operator |
| `riley.recycling@capital-demo.com` | Recycling operator |
| `casey.additive@capital-demo.com` | Compounding operator |
| `taylor.mixing@capital-demo.com` | Mixing operator |
| `sam.paving@capital-demo.com` | Site manager |
| `quinn.lab@capital-demo.com` | Lab technician |
| `avery.quality@capital-demo.com` | Quality manager |
| `drew.dispatch@capital-demo.com` | Dispatcher |
| `jamie.assets@capital-demo.com` | Road asset manager |
| `robin.reporter@capital-demo.com` | Reporter (read-only) |

Shared password: `roadpass-demo-2026`. The Route 740 sections carry public MoDOT figures
(`PublicReference`); everything else is `Simulated`. Details: [`docs/DEMO_WORKSPACE.md`](docs/DEMO_WORKSPACE.md).

## Quick start

Needs Docker (OrbStack on macOS), Go 1.27, Node 24.

```bash
make stack        # build, start, seed ~2.9M simulated rows, seal on Fabric → http://localhost:8080
make e2e          # the same, at scale 0.3, plus the Playwright suite
make debug        # Hyperledger Explorer on http://localhost:8091 (debugging only)
make stack-clean  # remove everything, ledger included
```

Native development against containerised data services:

```bash
make infra        # Postgres :15433, ClickHouse :19100, Redis :16379
go run ./cmd/roadpass seed -reset -scale 0.3
make web dev      # dashboard + server on http://127.0.0.1:8090 (DEMO_MODE)
cd web && npm run dev   # hot reload on :5173, proxies the API to :8090
```

`make check` runs gofmt, vet, the Go and chaincode tests, and the TypeScript check.

## Deployment

One image ([`deploy/Dockerfile`](deploy/Dockerfile), distroless, ~52 MB) runs both processes:
`roadpass serve` (dashboard, REST, NGSI-LD) and `roadpass worker` (projector, sealer, telemetry).
They share PostgreSQL 17, ClickHouse ≥ 25, Redis 7 and a Hyperledger Fabric 2.5 peer.
Full reference: [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md).

### Any Docker host

```bash
export ENVIRONMENT=production
export PUBLIC_URL=https://roadpass.example.org
export SIGNING_MASTER_KEY=$(openssl rand -hex 32)   # keep it in your secret store; losing it means re-issuing every key
docker compose -f deploy/compose.yml up -d          # serve and worker migrate on start
curl -fsS https://roadpass.example.org/readyz       # dependencies healthy
```

Optional demo data (≈2.9M simulated rows at scale 1, about 75 s):

```bash
docker compose -f deploy/compose.yml --profile seed run --rm seed
```

### Coolify (how roadpass.sensifai.com runs)

1. **New resource → Docker Compose** from this repository, Docker Compose Location `/docker-compose.coolify.yml`.
2. Give the `app` service its domain; Traefik reaches it on port 8080. No other service is public.
3. Set `SIGNING_MASTER_KEY` and `CLICKHOUSE_PASSWORD` (and `CLICKHOUSE_HOST` if not the shared one). Coolify generates the database passwords.
4. Optional: `TURNSTILE_SITE_KEY` / `TURNSTILE_SECRET_KEY` for the sign-in and contact forms; `DEMO_TENANT=1` (on by default) for the Capital Paving workspace.

Steps and secrets: [`deploy/coolify/README.md`](deploy/coolify/README.md).

### Operations

- **Health:** `GET /healthz` (process), `GET /readyz` (PostgreSQL, ClickHouse, Redis, Fabric).
- **Scaling:** run more `app` replicas freely; extra `worker` replicas are safe.
- **Backups:** PostgreSQL and the Fabric volumes (`fabric-data`, `fabric-crypto`). ClickHouse is rebuilt with `roadpass project -rebuild`; Redis is a cache.

## Examples

Create an API key in the dashboard (**Data in → API keys**, admins only) with the scopes and
duties it needs, then send it as a bearer token. The OpenAPI description is at `/api/openapi.json`.

```bash
export RP=https://roadpass.example.org
export KEY=rpk_live_...   # API key secret, shown once
```

**Submit a record (JSON).** An `Idempotency-Key` makes retries safe.

```bash
curl -X POST "$RP/api/v1/records" \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -H "Idempotency-Key: PRL-2026-0001" \
  -d '{"records":[{"type":"reject_lot","ref":"PRL-2026-0001","siteId":"site_my-plant_1",
       "occurredAt":"2026-11-05T09:30:00Z","massKg":18000,
       "data":{"source":"municipal_packaging","ldpe_share_pct":74,"avoided_route":"landfill",
               "moisture_pct":10,"contamination_pct":9}}]}'
```

The answer lists each record's id and the findings of the data checks C1–C9; blockers return `422`.

**Import a CSV.** Download the template for a record type from **Data in**; `;` or `,` separated.

```bash
printf 'ref;site_id;occurred_at;mass_kg;source;ldpe_share_pct;avoided_route\nPRL-2026-0002;site_my-plant_1;2026-11-05 10:00;19000;municipal_packaging;72,5;landfill\n' > lots.csv
curl -X POST "$RP/api/v1/imports?type=reject_lot" -H "Authorization: Bearer $KEY" -F file=@lots.csv
```

**Create an entity with NGSI-LD** (normalized or concise in, `keyValues` out).

```bash
curl -X POST "$RP/ngsi-ld/v1/entities" \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/ld+json" \
  -d '{"@context":"'"$RP"'/ngsi-ld/v1/context.jsonld","type":"PlasticRejectLot",
       "reference":"PRL-2026-0003","dateObserved":"2026-11-05T11:00:00Z",
       "mass":{"type":"Property","value":17.5,"unitCode":"TNE"},
       "atSite":{"type":"Relationship","object":"urn:ngsi-ld:Site:site_my-plant_1"},
       "source":"commercial_film","ldpeSharePct":80,"avoidedRoute":"incineration"}'

curl "$RP/ngsi-ld/v1/entities?type=PlasticRejectLot&options=keyValues" -H "Authorization: Bearer $KEY"
```

**Read a passport and its proof.**

```bash
curl "$RP/api/v1/passports/RSC-26-000001" -H "Authorization: Bearer $KEY"        # journey, lineage, recycled content, CO₂e
curl "$RP/api/v1/records/RSC-26-000001/proof" -H "Authorization: Bearer $KEY"    # fingerprint, signatures, Merkle path, Fabric tx
```

**Verify a fingerprint against the ledger** (public, no account needed).

```bash
curl -X POST "$RP/api/v1/ledger/verify" -H "Content-Type: application/json" \
  -d '{"hash":"<sha-256 fingerprint from the passport>"}'
```

Change one byte of a record and the same call returns `found: false`. The browser does the same
check on **Verify in this browser** and on `/verify`.

**Export.**

```bash
curl "$RP/api/v1/exports/greta.csv" -H "Authorization: Bearer $KEY" -o greta.csv   # GRETA-ready LCA data
roadpass circuloos -env circuloos.env -tenant circuloos_roadpass -dry-run        # NGSI-LD to the CIRCULOOS Data Platform
```

## The binary

| Command | What |
|---|---|
| `roadpass serve [--worker]` | dashboard, REST `/api/v1`, NGSI-LD `/ngsi-ld/v1`, OpenAPI at `/api/openapi.json` |
| `roadpass worker` | ClickHouse projector, ledger sealer and key registration, telemetry stream |
| `roadpass seed -reset -scale N` | demo dataset: a simulated Spanish chain 2024-01 → 2026-09 (deterministic per seed) |
| `roadpass project [-rebuild]` | re-project Postgres into ClickHouse |
| `roadpass ledger` | register keys and seal everything sealable now |
| `roadpass circuloos [-dry-run]` | export sealed passports to the CIRCULOOS Data Platform, read back, verify against the ledger |
| `roadpass migrate`, `roadpass health [-worker]` | schema migration; container health checks |
