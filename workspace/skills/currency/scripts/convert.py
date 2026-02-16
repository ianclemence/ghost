import sys
import requests

def convert(amount, from_curr, to_curr):
    url = f"https://open.er-api.com/v6/latest/{from_curr}"
    try:
        response = requests.get(url)
        data = response.json()
        
        if 'rates' not in data:
            print(f"Error: Could not fetch rates for {from_curr}")
            return

        rate = data['rates'].get(to_curr)
        if rate:
            result = amount * rate
            print(f"{amount} {from_curr} = {result:.2f} {to_curr}")
            print(f"Rate: 1 {from_curr} = {rate} {to_curr}")
        else:
            print(f"Currency {to_curr} not found in rates.")
            
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    if len(sys.argv) < 4:
        print("Usage: python convert.py <amount> <from> <to>")
        print("Example: python convert.py 100 USD EUR")
    else:
        try:
            amt = float(sys.argv[1])
            convert(amt, sys.argv[2].upper(), sys.argv[3].upper())
        except ValueError:
            print("Error: Amount must be a number.")
