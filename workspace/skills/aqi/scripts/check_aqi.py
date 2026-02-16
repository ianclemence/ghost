import sys
import requests

def get_status(aqi):
    if aqi is None: return "Unknown"
    if aqi <= 50: return "Good 🟢"
    if aqi <= 100: return "Moderate 🟡"
    if aqi <= 150: return "Unhealthy for Sensitive Groups 🟠"
    if aqi <= 200: return "Unhealthy 🔴"
    return "Hazardous 🟣"

def get_aqi(city):
    # 1. Geocoding
    geo_url = f"https://geocoding-api.open-meteo.com/v1/search?name={city}&count=1&language=en&format=json"
    try:
        geo_res = requests.get(geo_url).json()
        if not geo_res.get('results'):
            print(f"City '{city}' not found.")
            return
        
        lat = geo_res['results'][0]['latitude']
        lon = geo_res['results'][0]['longitude']
        name = geo_res['results'][0]['name']
        country = geo_res['results'][0].get('country', '')

        # 2. AQI
        aqi_url = f"https://air-quality-api.open-meteo.com/v1/air-quality?latitude={lat}&longitude={lon}&current=us_aqi,pm10,pm2_5"
        aqi_res = requests.get(aqi_url).json()
        
        current = aqi_res.get('current', {})
        
        print(f"--- Air Quality in {name}, {country} ---")
        print(f"US AQI: {current.get('us_aqi')} ({get_status(current.get('us_aqi'))})")
        print(f"PM2.5:  {current.get('pm2_5')} μg/m³")
        print(f"PM10:   {current.get('pm10')} μg/m³")

    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python check_aqi.py <city>")
    else:
        get_aqi(" ".join(sys.argv[1:]))
