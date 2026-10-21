// Room climate reader for Ghost.
//
// Reads a DHT11 and prints one JSON line per reading to the serial port at
// 9600 baud. The bridge on the Pod (room-climate-bridge.py) turns those
// lines into a latest-reading file and into Ghost's sensor inbox.
//
// The data pin is not assumed: at boot the sketch walks the safe digital
// pins looking for a responding DHT11 and locks onto the first one that
// answers, so a sensor wired to a neighbouring pin still works. While
// scanning it reports {"scan":N}; if nothing answers it says so every cycle
// instead of going quiet.
//
// The Uno's own LED (pin 13) reports status without a terminal:
//   solid ON   - sensor found, readings are flowing
//   slow blink - scanning for the sensor
//   3 fast blinks, then dark - no sensor answered (check the wiring)
//
// Wiring:  DHT11 VCC -> 5V, DHT11 DATA -> any digital pin (not 13), GND -> GND.
// Fallback power: if the 5V/GND path cannot be trusted, the board can feed
// the sensor itself - move VCC to A4 and GND to A5 (both are driven here and
// are never scanned as data pins), so a working sensor answers either way.

#include <DHT.h>

#define DHTTYPE DHT11
#define INTERVAL_MS 5000UL   // DHT11 is spec-limited to 1 Hz; 5 s is safe.
#define RETRIES_TO_RESCAN 12 // give up on a pin after this many failures.

// D2..D12 first (the pins the guide names), then A0..A3 used as digital.
// D13 is left out: it drives the board's own status LED.
const uint8_t CANDIDATES[] = {2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 15, 16, 17};

// Last resort, probed only when every safe pin came up empty: pins 0 (RX)
// and 1 (TX) are the link to the computer, so a sensor parked there is a
// wiring mistake worth reporting by name rather than silently missed.
const uint8_t SERIAL_PINS[] = {0, 1};
const uint8_t SERIAL_PIN_COUNT = sizeof(SERIAL_PINS);
const uint8_t CANDIDATE_COUNT = sizeof(CANDIDATES);

DHT *sensor = nullptr;
uint8_t sensorPin = 0;
uint8_t failures = 0;

// One probing attempt on a pin: two reads a moment apart, because the DHT11
// ignores requests issued too soon after the previous one.
bool probe(uint8_t pin, float &t, float &h) {
  DHT candidate(pin, DHTTYPE);
  candidate.begin();
  delay(1300);
  h = candidate.readHumidity();
  t = candidate.readTemperature();
  if (isnan(h) || isnan(t)) {
    delay(1300);
    h = candidate.readHumidity();
    t = candidate.readTemperature();
  }
  return !(isnan(h) || isnan(t));
}

void attach(uint8_t pin) {
  sensorPin = pin;
  sensor = new DHT(pin, DHTTYPE);
  sensor->begin();
}

void detach() {
  delete sensor;
  sensor = nullptr;
  sensorPin = 0;
  failures = 0;
}

void led(bool on) { digitalWrite(LED_BUILTIN, on ? HIGH : LOW); }

// Three quick flashes: the board cannot see a sensor on any pin.
void ledMissing() {
  led(false);
  for (uint8_t i = 0; i < 3; i++) {
    led(true);
    delay(90);
    led(false);
    delay(90);
  }
}

// Attach to a pin that answered and report it (with a named fix when the
// sensor sits on a pin that belongs to the computer link).
void adopt(uint8_t pin, float t, float h, bool serialPin) {
  attach(pin);
  led(true);                          // solid: sensor found
  Serial.print(F("{\"found\":"));
  Serial.print(pin);
  if (serialPin) {
    Serial.print(F(",\"warning\":\"sensor-on-serial-pin\",\"fix\":\"move DATA wire to D2\""));
  }
  Serial.println(F("}"));
  Serial.print(F("{\"t\":"));
  Serial.print(t, 1);
  Serial.print(F(",\"h\":"));
  Serial.print(h, 1);
  Serial.print(F(",\"pin\":"));
  Serial.print(pin);
  Serial.println(F("}"));
}

void scanForSensor() {
  for (uint8_t i = 0; i < CANDIDATE_COUNT; i++) {
    float t, h;
    led(!digitalRead(LED_BUILTIN));   // slow blink while hunting
    Serial.print(F("{\"scan\":"));
    Serial.print(CANDIDATES[i]);
    Serial.println(F("}"));
    if (probe(CANDIDATES[i], t, h)) {
      adopt(CANDIDATES[i], t, h, false);
      return;
    }
  }

  // Nothing on a safe pin: is it parked on the link to the computer?
  for (uint8_t i = 0; i < SERIAL_PIN_COUNT; i++) {
    float t, h;
    led(!digitalRead(LED_BUILTIN));
    Serial.print(F("{\"scan-serial\":"));
    Serial.print(SERIAL_PINS[i]);
    Serial.println(F("}"));
    if (probe(SERIAL_PINS[i], t, h)) {
      adopt(SERIAL_PINS[i], t, h, true);
      return;
    }
  }

  Serial.println(F("{\"err\":\"no-sensor\"}"));
  ledMissing();
}

void setup() {
  pinMode(LED_BUILTIN, OUTPUT);
  led(false);
  // Optional self-powered wiring: A4 = 5V, A5 = GND. Driving them costs
  // nothing when no sensor is attached (DHT11 draws a few mA).
  pinMode(A4, OUTPUT);
  digitalWrite(A4, HIGH);
  pinMode(A5, OUTPUT);
  digitalWrite(A5, LOW);
  Serial.begin(9600);
  scanForSensor();
}

void loop() {
  delay(INTERVAL_MS);

  if (sensor == nullptr) {
    scanForSensor();          // keep looking: the wiring may still be fixed
    return;
  }

  float t = sensor->readTemperature();
  float h = sensor->readHumidity();

  if (isnan(t) || isnan(h)) {
    led(false);                 // dark = reading problem
    Serial.print(F("{\"err\":\"no-read\",\"pin\":"));
    Serial.print(sensorPin);
    Serial.println(F("}"));
    if (++failures >= RETRIES_TO_RESCAN) {
      detach();               // that pin stopped answering; look again
    }
    return;
  }

  failures = 0;
  led(true);                   // solid = readings are flowing
  Serial.print(F("{\"t\":"));
  Serial.print(t, 1);
  Serial.print(F(",\"h\":"));
  Serial.print(h, 1);
  Serial.print(F(",\"pin\":"));
  Serial.print(sensorPin);
  Serial.println(F("}"));
}
