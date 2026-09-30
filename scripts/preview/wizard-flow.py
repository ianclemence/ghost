#!/usr/bin/env python3
"""Drive the first-run wizard in headless Chromium and shoot each step.
   scripts/preview/wizard-flow.py PORT CODE OUTDIR [WIDTH]"""
import json, os, subprocess, sys
port, code, out = sys.argv[1:4]; width = sys.argv[4] if len(sys.argv) > 4 else "390"
H = 'const set=(p,v)=>{const i=[...document.querySelectorAll("input")].find(x=>x.placeholder===p);i.value=v;i.dispatchEvent(new Event("input",{bubbles:true}))};const btn=(t)=>[...document.querySelectorAll("button")].find(b=>b.textContent.trim()===t).click();'
def step(body, name, wait=700, log=None):
    d = {"js": "(()=>{" + H + body + "})()", "wait": wait, "out": f"{out}/{name}.png"}
    if log: d["log"] = log
    return d
wrong = "000000" if code != "000000" else "111111"
pw = 'set("Password","correct horse 88");set("Confirm password","correct horse 88");btn("Continue")'
steps = [
  step(f'set("Setup code","{wrong}");btn("Set up Ghost")', "01-identity"),
  step('set("Your name","Sam");btn("Continue")', "02-password"),
  step(pw, "03-brain"),
  step('[...document.querySelectorAll(".wizard-choice")].find(b=>/On this device/.test(b.textContent)).click();btn("Continue")', "04-wrong-code-bounce", 5000),
  step(f'set("Setup code","{code}");btn("Set up Ghost")', "05-identity-again"),
  step('set("Your name","Sam");btn("Continue")', "06-password-again"),
  step(pw, "07-brain-again"),
  step('[...document.querySelectorAll(".wizard-choice")].find(b=>/On this device/.test(b.textContent)).click();btn("Continue")', "08-claimed", 6000),
  step('btn("Finish setup")', "09-done", 1500),
]
env = dict(os.environ, STEPS=json.dumps(steps))
os.makedirs(out, exist_ok=True)
subprocess.run(["bun", "scripts/preview/shot.mjs", f"http://127.0.0.1:{port}/", "/tmp/x.png", "2500", width, "800"], env=env)
