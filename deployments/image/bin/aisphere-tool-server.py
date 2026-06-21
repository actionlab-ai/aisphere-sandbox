#!/usr/bin/env python3
import json, os, pathlib, subprocess
from http.server import BaseHTTPRequestHandler, HTTPServer
WORKSPACE = pathlib.Path(os.environ.get('AISPHERE_WORKSPACE','/workspace')).resolve()
PORT = int(os.environ.get('AISPHERE_TOOL_PORT','18081'))
ALLOW_SHELL = os.environ.get('AISPHERE_ALLOW_SHELL','false').lower() == 'true'
TOOLS = [
 {'name':'workspace.list','description':'List files under /workspace','inputSchema':{'type':'object','properties':{'path':{'type':'string'},'recursive':{'type':'boolean'}}}},
 {'name':'workspace.read','description':'Read UTF-8 file from /workspace','inputSchema':{'type':'object','required':['path'],'properties':{'path':{'type':'string'},'maxBytes':{'type':'integer'}}}},
 {'name':'workspace.write','description':'Write UTF-8 file to /workspace','inputSchema':{'type':'object','required':['path','content'],'properties':{'path':{'type':'string'},'content':{'type':'string'}}}},
 {'name':'workspace.search_text','description':'Search text under /workspace','inputSchema':{'type':'object','required':['query'],'properties':{'query':{'type':'string'},'path':{'type':'string'}}}},
 {'name':'shell.exec','description':'Run shell command when enabled','inputSchema':{'type':'object','required':['command'],'properties':{'command':{'type':'string'},'timeoutSeconds':{'type':'integer'}}}},
]
def safe(p):
    target=(WORKSPACE / (p or '.')).resolve()
    if not str(target).startswith(str(WORKSPACE)): raise ValueError('path escapes workspace')
    return target
def ok(v): return {'ok': True, 'result': v}
def fail(e): return {'ok': False, 'error': {'code': type(e).__name__, 'message': str(e)}}
def call(tool, inp):
    try:
        if tool=='workspace.list':
            root=safe(inp.get('path','.')); rec=inp.get('recursive',False)
            items=[]; it=root.rglob('*') if rec else root.iterdir()
            for x in it: items.append({'path':str(x.relative_to(WORKSPACE)),'type':'dir' if x.is_dir() else 'file','size':x.stat().st_size if x.is_file() else 0})
            return ok({'items':items})
        if tool=='workspace.read':
            b=safe(inp['path']).read_bytes()[:int(inp.get('maxBytes',1048576))]
            return ok({'content':b.decode('utf-8','replace')})
        if tool=='workspace.write':
            p=safe(inp['path']); p.parent.mkdir(parents=True, exist_ok=True); p.write_text(inp.get('content',''))
            return ok({'path':str(p.relative_to(WORKSPACE)),'bytes':p.stat().st_size})
        if tool=='workspace.search_text':
            q=inp['query']; root=safe(inp.get('path','.')); hits=[]
            for f in root.rglob('*'):
                if f.is_file():
                    try:
                        for i,line in enumerate(f.read_text(errors='ignore').splitlines(),1):
                            if q in line: hits.append({'path':str(f.relative_to(WORKSPACE)),'line':i,'text':line[:500]})
                    except Exception: pass
            return ok({'hits':hits[:200]})
        if tool=='shell.exec':
            if not ALLOW_SHELL: raise PermissionError('shell.exec disabled')
            r=subprocess.run(inp['command'], shell=True, cwd=str(WORKSPACE), capture_output=True, text=True, timeout=int(inp.get('timeoutSeconds',30)))
            return ok({'exitCode':r.returncode,'stdout':r.stdout[-20000:],'stderr':r.stderr[-20000:]})
        raise KeyError(tool)
    except Exception as e: return fail(e)
class H(BaseHTTPRequestHandler):
    def send(self, code, obj):
        data=json.dumps(obj,ensure_ascii=False).encode(); self.send_response(code); self.send_header('Content-Type','application/json'); self.send_header('Content-Length',str(len(data))); self.end_headers(); self.wfile.write(data)
    def do_GET(self):
        if self.path=='/healthz': return self.send(200, {'ok':True})
        if self.path=='/v1/tools': return self.send(200, {'tools':TOOLS})
        self.send(404, {'error':'not found'})
    def do_POST(self):
        if self.path!='/v1/tools/call': return self.send(404, {'error':'not found'})
        body=json.loads(self.rfile.read(int(self.headers.get('Content-Length','0') or 0)) or b'{}')
        self.send(200, call(body.get('tool'), body.get('input') or {}))
if __name__=='__main__':
    WORKSPACE.mkdir(parents=True, exist_ok=True)
    HTTPServer(('0.0.0.0', PORT), H).serve_forever()
