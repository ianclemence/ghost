#!/usr/bin/env python3
"""Offline unit converter. No network. Source of truth for factors."""
import sys

LENGTH = {"mm": 0.001, "cm": 0.01, "m": 1.0, "km": 1000.0,
          "in": 0.0254, "ft": 0.3048, "yd": 0.9144, "mi": 1609.344}
WEIGHT = {"g": 1.0, "kg": 1000.0, "oz": 28.3495, "lbs": 453.592, "lb": 453.592}
VOLUME = {"ml": 1.0, "l": 1000.0, "tsp": 4.92892, "tbsp": 14.7868,
          "cups": 236.588, "cup": 236.588, "pints": 473.176, "pint": 473.176,
          "quarts": 946.353, "quart": 946.353, "gallons": 3785.41, "gallon": 3785.41}

ALIASES = {"celsius": "C", "fahrenheit": "F", "kelvin": "K",
           "tablespoon": "tbsp", "tablespoons": "tbsp",
           "teaspoon": "tsp", "teaspoons": "tsp",
           "pound": "lbs", "pounds": "lbs", "kilo": "kg", "kilos": "kg",
           "liter": "l", "liters": "l", "litre": "l", "litres": "l",
           "inch": "in", "inches": "in", "foot": "ft", "feet": "ft",
           "cup": "cups", "ounce": "oz", "ounces": "oz"}


def norm(u):
    u = u.strip().lower()
    return ALIASES.get(u, u)


def convert_temp(amount, frm, to):
    frm, to = frm.upper(), to.upper()
    if frm == to:
        return amount
    if frm == "C":
        c = amount
    elif frm == "F":
        c = (amount - 32) * 5 / 9
    elif frm == "K":
        c = amount - 273.15
    else:
        return None
    if to == "C":
        return c
    if to == "F":
        return c * 9 / 5 + 32
    if to == "K":
        return c + 273.15
    return None


def convert(amount, frm, to):
    frm, to = norm(frm), norm(to)
    if frm.upper() in ("C", "F", "K") or to.upper() in ("C", "F", "K"):
        r = convert_temp(amount, frm, to)
        if r is not None:
            return f"{amount:g} {frm} = {r:.2f} {to}"
        return None
    for table in (LENGTH, WEIGHT, VOLUME):
        if frm in table and to in table:
            base = amount * table[frm]
            return f"{amount:g} {frm} = {base / table[to]:.2f} {to}"
    return None


def main():
    if len(sys.argv) != 4:
        print("Usage: convert.py <amount> <from> <to>")
        print("Example: convert.py 2 cups ml")
        sys.exit(1)
    try:
        amount = float(sys.argv[1])
    except ValueError:
        print(f"Error: '{sys.argv[1]}' is not a number.")
        sys.exit(1)
    r = convert(amount, sys.argv[2], sys.argv[3])
    if r is None:
        print(f"Error: cannot convert '{sys.argv[2]}' to '{sys.argv[3]}'.")
        print("Length: mm, cm, m, km, in, ft, yd, mi")
        print("Weight: g, kg, oz, lbs")
        print("Volume: ml, l, tsp, tbsp, cups, pints, quarts, gallons")
        print("Temperature: C, F, K")
        sys.exit(1)
    print(r)


if __name__ == "__main__":
    main()
