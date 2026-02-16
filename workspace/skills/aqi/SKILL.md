# Air Quality Index (AQI) & Pollen Skill

## Description
Check the current Air Quality Index (AQI) and Pollen levels for a specific location using open APIs.

## Tools
- **Geocoding API**: `https://geocoding-api.open-meteo.com/v1/search?name={CITY}`
- **Air Quality API**: `https://air-quality-api.open-meteo.com/v1/air-quality?latitude={LAT}&longitude={LON}&current=us_aqi,pm10,pm2_5,carbon_monoxide,nitrogen_dioxide,sulphur_dioxide,ozone,aerosol_optical_depth,dust,uv_index`

## Cross-Platform Method (Python)
Works on Windows, Linux, and Mac.

1.  **Run Script**:
    ```bash
    python workspace/skills/aqi/scripts/check_aqi.py "New York"
    ```

## Workflow (PowerShell Only)
1.  **Get Location**:
    - Ask the user for their City (e.g., "New York").
    - Fetch coordinates:
      ```powershell
      curl "https://geocoding-api.open-meteo.com/v1/search?name=New+York&count=1&language=en&format=json"
      ```
    - Extract `latitude` and `longitude` from the JSON response.

2.  **Fetch AQI Data**:
    - Use the coordinates to query the Air Quality API.
      ```powershell
      curl "https://air-quality-api.open-meteo.com/v1/air-quality?latitude=40.71&longitude=-74.01&current=us_aqi,pm2_5,pm10"
      ```

3.  **Report**:
    - Display the **US AQI**, **PM2.5**, and **PM10** values clearly to the user.
    - Interpret the AQI (e.g., 0-50 Good, 51-100 Moderate, >100 Unhealthy).

## Alternative (Simpler)
- Use `wttr.in` if only weather/simple info is needed:
  ```powershell
  curl wttr.in/New_York
  ```
  *(Note: wttr.in is primarily weather, but sometimes includes basic environmental info. The API method above is preferred for specific AQI tasks.)*
