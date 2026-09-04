#!/usr/bin/env python3
"""Safe local calculator. No network. Only arithmetic, never exec input."""
import ast
import operator
import re
import sys

OPS = {ast.Add: operator.add, ast.Sub: operator.sub, ast.Mult: operator.mul,
       ast.Div: operator.truediv, ast.Mod: operator.mod, ast.Pow: operator.pow,
       ast.USub: operator.neg, ast.UAdd: operator.pos}


def safe_eval(expr):
    expr = expr.strip().replace(",", "").replace("×", "*").replace("÷", "/")
    if not re.fullmatch(r"[\d\s\.\+\-\*\/\%\(\)]+", expr):
        raise ValueError("unsupported characters")
    node = ast.parse(expr, mode="eval")

    def ev(n):
        if isinstance(n, ast.Expression):
            return ev(n.body)
        if isinstance(n, ast.Constant) and isinstance(n.value, (int, float)):
            return n.value
        if isinstance(n, ast.BinOp) and type(n.op) in OPS:
            return OPS[type(n.op)](ev(n.left), ev(n.right))
        if isinstance(n, ast.UnaryOp) and type(n.op) in OPS:
            return OPS[type(n.op)](ev(n.operand))
        raise ValueError("unsupported expression")

    return ev(node)


def fmt(n):
    if abs(n - round(n)) < 1e-9:
        return str(int(round(n)))
    return f"{n:,.2f}"


def main():
    if len(sys.argv) != 2:
        print('Usage: calc.py "<expression>"')
        print('Examples: "48*17+3", "15% of 850", "15% tip on 850 split 3"')
        sys.exit(1)
    s = sys.argv[1].strip()
    low = s.lower()
    # "15% tip on 850 split 3"
    m = re.match(r"(\d+(?:\.\d+)?)%\s*tip\s*on\s*([\d,\.]+)(?:\s*split\s*(\d+))?", low)
    if m:
        pct, base = float(m.group(1)), float(m.group(2).replace(",", ""))
        tip, total = base * pct / 100, base * (1 + pct / 100)
        out = f"{pct:g}% tip on {fmt(base)} = tip {fmt(tip)}, total {fmt(total)}"
        if m.group(3):
            n = int(m.group(3))
            out += f", each pays {fmt(total / n)} (split {n} ways)"
        print(out)
        return
    # "15% of 850"
    m = re.match(r"(\d+(?:\.\d+)?)%\s*of\s*([\d,\.]+)", low)
    if m:
        print(f"{m.group(1)}% of {m.group(2)} = {fmt(float(m.group(1)) * float(m.group(2).replace(',', '')) / 100)}")
        return
    # "split 1200 3 ways" / "split 1200 three ways"
    m = re.match(r"split\s*([\d,\.]+)\s*(\d+)\s*ways?", low)
    if m:
        base, n = float(m.group(1).replace(",", "")), int(m.group(2))
        print(f"{fmt(base)} split {n} ways = {fmt(base / n)} each")
        return
    try:
        print(f"{s} = {fmt(safe_eval(s))}")
    except Exception:
        print("Error: cannot parse. Try '48*17+3', '15% of 850', or '15% tip on 850 split 3'.")
        sys.exit(1)


if __name__ == "__main__":
    main()
