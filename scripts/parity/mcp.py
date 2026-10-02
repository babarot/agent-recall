#!/usr/bin/env python3
"""Compare the MCP servers of the TypeScript and Go implementations.

Both run over stdio against their own copy of one archive snapshot and one
transcript snapshot. tools/list must describe the same tools, and every
tools/call must return the same text.

Usage: scripts/parity/mcp.py [path/to/vault.db]
"""
import json, os, shutil, subprocess, sys, tempfile

root = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
src_db = sys.argv[1] if len(sys.argv) > 1 else os.path.expanduser("~/.claude/vault.db")
work = tempfile.mkdtemp()

def clone(a, b):
    if subprocess.run(["cp", "-cR", a, b], stderr=subprocess.DEVNULL).returncode:
        shutil.copytree(a, b)

clone(os.path.expanduser("~/.claude/projects"), f"{work}/projects")
subprocess.run(["sqlite3", src_db, f".backup '{work}/vault.db'"], check=True)
subprocess.run(["go", "build", "-o", f"{work}/recall-go", f"{root}/cmd/recall"], check=True)
deno_dir = json.loads(subprocess.run(["deno", "info", "--json"], capture_output=True, text=True).stdout)["denoDir"]

def start(side):
    home = f"{work}/{side}/home"
    os.makedirs(f"{home}/.claude")
    clone(f"{work}/projects", f"{home}/.claude/projects")
    shutil.copy(f"{work}/vault.db", f"{work}/{side}/vault.db")
    env = dict(os.environ, HOME=home, DENO_DIR=deno_dir)
    if side == "ts":
        cmd = ["deno", "run", "--allow-read", "--allow-write", "--allow-env=HOME", "--allow-net", "--allow-run",
               f"{root}/src/main.ts", "mcp", "--db", f"{work}/ts/vault.db"]
    else:
        cmd = [f"{work}/recall-go", "mcp", "--db", f"{work}/go/vault.db"]
    return subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, env=env, text=True)

def request(p, rid, method, params=None):
    msg = {"jsonrpc": "2.0", "id": rid, "method": method}
    if params is not None:
        msg["params"] = params
    p.stdin.write(json.dumps(msg) + "\n"); p.stdin.flush()
    while True:
        line = p.stdout.readline()
        if not line:
            raise SystemExit(f"server exited waiting for {method}")
        try:
            resp = json.loads(line)
        except json.JSONDecodeError:
            continue  # the TypeScript server prints its import summary to stdout
        if resp.get("id") == rid:
            return resp

def notify(p, method):
    p.stdin.write(json.dumps({"jsonrpc": "2.0", "method": method}) + "\n"); p.stdin.flush()

prefix = subprocess.run(["sqlite3", f"{work}/vault.db", "SELECT substr(session_id,1,8) FROM sessions ORDER BY ended_at DESC LIMIT 1 OFFSET 4"],
                        capture_output=True, text=True).stdout.strip()
calls = [
    ("recall_search", {"query": "terraform"}),
    ("recall_search", {"query": "error", "limit": 5, "project": "dotfiles"}),
    ("recall_search", {"query": "deploy", "from": "2026-06-01", "to": "2026-07-01", "limit": 30}),
    ("recall_list", {}),
    ("recall_list", {"limit": 3, "project": "agent"}),
    ("recall_export", {"session_id": prefix}),
    ("recall_export", {"session_id": "nope"}),
    ("recall_stats", {}),
    ("recall_stats", {"project": "dotfiles"}),
]

servers = {side: start(side) for side in ("ts", "go")}
fail = False
try:
    for side, p in servers.items():
        r = request(p, 1, "initialize", {"protocolVersion": "2024-11-05", "capabilities": {},
                                         "clientInfo": {"name": "parity", "version": "0"}})
        print(f"info  {side}: protocol {r['result']['protocolVersion']}, server {r['result']['serverInfo']}")
        notify(p, "notifications/initialized")

    tools = {}
    for side, p in servers.items():
        tools[side] = sorted(({"name": t["name"], "description": t["description"], "inputSchema": t["inputSchema"]}
                              for t in request(p, 2, "tools/list")["result"]["tools"]), key=lambda t: t["name"])
    if json.dumps(tools["ts"], sort_keys=True) == json.dumps(tools["go"], sort_keys=True):
        print(f"ok    tools/list ({len(tools['go'])} tools)")
    else:
        fail = True
        print("DIFF  tools/list")
        print(" ts:", json.dumps(tools["ts"], sort_keys=True)[:400])
        print(" go:", json.dumps(tools["go"], sort_keys=True)[:400])

    for i, (name, args) in enumerate(calls, start=10):
        out = {}
        for side, p in servers.items():
            res = request(p, i, "tools/call", {"name": name, "arguments": args})["result"]
            out[side] = (res["content"][0]["text"], bool(res.get("isError")))
        if out["ts"] == out["go"]:
            print(f"ok    {name} {json.dumps(args)} ({len(out['go'][0])} chars)")
        else:
            fail = True
            print(f"DIFF  {name} {json.dumps(args)}")
            a, b = out["ts"][0], out["go"][0]
            at = next((k for k in range(min(len(a), len(b))) if a[k] != b[k]), min(len(a), len(b)))
            print("  ts:", repr(a[max(0, at - 80):at + 80]))
            print("  go:", repr(b[max(0, at - 80):at + 80]))
finally:
    for p in servers.values():
        p.stdin.close(); p.terminate()
    shutil.rmtree(work, ignore_errors=True)
sys.exit(1 if fail else 0)
