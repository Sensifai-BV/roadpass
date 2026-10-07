#!/usr/bin/env python3
"""Walks one lot through the recycler step with a different person per duty:
sorting-plant dispatch ships a reject lot -> recycler intake receives it ->
production records a recycled lot -> quality tests and releases it -> dispatch
ships it to an additive producer. Also checks that duties are enforced.
Usage: scripts/workflow.py [base-url]   (server with DEMO_MODE=1)"""
import json, sys, time, urllib.request, http.cookiejar
from datetime import datetime, timezone

B = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8090"
users = json.load(urllib.request.urlopen(B + "/api/v1/auth/demo-users"))

class Session:
    def __init__(self, org, pick):
        self.jar = http.cookiejar.CookieJar()
        self.op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))
        u = next(x for x in users if x["orgId"] == org and pick(x))
        self.name = f'{u["name"]} ({org}, {u["role"]} {",".join(u["duties"])})'
        self.call("POST", "/auth/demo-login", {"userId": u["userId"]})
    def call(self, method, path, body=None, expect=None):
        req = urllib.request.Request(B + "/api/v1" + path, method=method,
            data=None if body is None else json.dumps(body).encode(),
            headers={"Content-Type": "application/json", "X-Requested-With": "roadpass"})
        try:
            with self.op.open(req) as r:
                code, data = r.status, r.read()
        except urllib.error.HTTPError as e:
            code, data = e.code, e.read()
        if expect and code != expect:
            sys.exit(f"✗ {self.name} {method} {path}: {code} {data[:300]!r}")
        return code, (json.loads(data) if data else None)

def now(): return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
def ok(msg): print(f"\033[32m✓\033[0m {msg}")
duty = lambda d: (lambda x: x["role"] == "editor" and d in x["duties"])
admin = lambda x: x["role"] == "admin"

sorter = Session("org_sel-pinto", admin)
site = "site_sel-pinto_1"
_, r = sorter.call("POST", "/records", {"records": [{"type": "reject_lot", "ref": f"WF-{int(time.time())}", "siteId": site,
    "occurredAt": now(), "massKg": 19800, "data": {"source": "municipal_packaging", "ldpe_share_pct": 77, "avoided_route": "landfill",
    "moisture_pct": 11, "contamination_pct": 12}}]}, 201)
lot = r["results"][0]["id"]; ok(f"sorting plant recorded reject lot {lot}")
time.sleep(1)
_, r = sorter.call("POST", "/records", {"records": [{"type": "shipment", "ref": f"ALB-WF-{int(time.time())}", "siteId": site, "occurredAt": now(),
    "massKg": 19800, "inputs": [{"id": lot, "massKg": 19800}], "data": {"to_org_id": "org_nuevafilm", "distance_km": 610, "carrier": "Transportes Ruta Verde S.L."}}]}, 201)
ship = r["results"][0]["id"]; ok(f"sorting plant shipped it to the recycler: {ship}")

production = Session("org_nuevafilm", duty("produce"))
code, _ = production.call("POST", f"/shipments/{ship}/receipt", {"receivedAt": now(), "massKg": 19750})
assert code == 403, f"production must not sign receipts, got {code}"; ok("production cannot sign the receipt (no intake duty)")

intake = Session("org_nuevafilm", duty("receive"))
_, w = intake.call("GET", "/work", expect=200)
assert any(i["record"]["id"] == ship for i in w["queues"]["receive"]["items"]), "shipment not in intake queue"
ok("shipment is in intake's 'To receive' queue")
time.sleep(1)
intake.call("POST", f"/shipments/{ship}/receipt", {"receivedAt": now(), "massKg": 19750}, 200); ok("intake signed the receipt")

_, w = production.call("GET", "/work", expect=200)
assert any(i["record"]["id"] == lot for i in w["queues"]["process"]["items"]), "lot not ready to process"
ok("the lot is in production's 'To process' queue")
time.sleep(1)
_, r = production.call("POST", "/records", {"records": [{"type": "recycled_lot", "ref": f"R-WF-{int(time.time())}", "siteId": "site_nuevafilm_1",
    "occurredAt": now(), "massKg": 13100, "inputs": [{"id": lot, "massKg": 19800}],
    "data": {"form": "agglomerate", "mfi_g10min": 1.3, "moisture_pct": 0.9, "energy_kwh": 7600}}]}, 201)
rec = r["results"][0]; ok(f"production recorded recycled lot {rec['id']} (status {rec['status']}, findings {[f['code'] for f in rec.get('findings') or []]})")
code, _ = production.call("POST", "/records", {"records": [{"type": "release", "ref": "X", "occurredAt": now(), "massKg": 0,
    "data": {"subject_id": rec["id"], "decision": "released"}}]})
assert code == 422, f"production must not release, got {code}"; ok("production cannot release it (no quality duty)")

quality = Session("org_nuevafilm", duty("release"))
time.sleep(1)
quality.call("POST", "/records", {"records": [{"type": "lab_test", "ref": f"INF-WF-{int(time.time())}", "occurredAt": now(), "massKg": 0,
    "data": {"subject_id": rec["id"], "test": "mfi", "value": 1.31, "result": "pass", "lab": "in-house"}}]}, 201); ok("quality recorded a lab test")
quality.call("POST", "/records", {"records": [{"type": "release", "ref": f"REL-{rec['id']}", "occurredAt": now(), "massKg": 0,
    "data": {"subject_id": rec["id"], "decision": "released", "note": "Within specification."}}]}, 201); ok("quality released the lot")

dispatch = Session("org_nuevafilm", duty("ship"))
_, w = dispatch.call("GET", "/work", expect=200)
assert any(i["record"]["id"] == rec["id"] for i in w["queues"]["ship"]["items"]), "released lot not ready to ship"
ok("the lot is in dispatch's 'Ready to ship' queue")
time.sleep(1)
_, r = dispatch.call("POST", "/records", {"records": [{"type": "shipment", "ref": f"ALB-R-{int(time.time())}", "siteId": "site_nuevafilm_1",
    "occurredAt": now(), "massKg": 13100, "inputs": [{"id": rec["id"], "massKg": 13100}], "data": {"to_org_id": "org_asfaltix", "distance_km": 560}}]}, 201)
res = r["results"][0]; ok(f"dispatch shipped it to the additive producer: {res['id']} (findings {[f['code'] for f in res.get('findings') or []]})")

_, pp = dispatch.call("GET", f"/passports/{rec['id']}", expect=200)
kinds = [e["kind"] for e in pp["timeline"]]
assert "released" in kinds and "received" in kinds and "tested" in kinds, kinds
ok(f"passport {rec['id']} shows the hand-offs: {', '.join(sorted(set(kinds)))}")

reporter = Session("org_programme-office", lambda x: True)
_, st = reporter.call("GET", "/chain/status", expect=200)
code, _ = reporter.call("POST", "/records", {"records": [{"type": "reject_lot", "ref": "R", "occurredAt": now(), "massKg": 1, "data": {}}]})
assert code == 403, f"reporter must be read-only, got {code}"
ok(f"reporter sees {len([s for s in st if s['records']])} organisations' status and cannot write")
print("workflow passed")
