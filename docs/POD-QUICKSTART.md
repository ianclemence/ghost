# Your Pod, from the box to your first conversation

This is the path for someone who bought a Ghost Pod, or is turning their own
computer into one (see [What a Pod needs](HARDWARE.md)). It takes about ten
minutes and needs a phone and a network.

## 1. Plug it in

Connect power and, if you can, an Ethernet cable (Wi-Fi works too). Wait about a
minute. The Pod has no screen: it announces itself on your network as
`ghost.local`.

## 2. Open the setup page

On a phone or computer on the same network, open **http://ghost.local**. If that
name does not resolve (some routers block it), use the Pod's address from your
router's device list.

## 3. Prove you are there

The setup page asks for a code. This is what stops someone else on your network
from claiming your Pod first. The code is:

- on the card in the box (Pods from us), or
- in a file named `ghost-setup-code` on the Pod's SD card, which you can read by
  putting the card in any computer (Pods you built yourself), or
- printed in the Pod's log, if you have a keyboard on it:
  `journalctl -u ghost-web | grep "Setup code"`.

## 4. Choose how Ghost thinks

Ghost needs an AI service to understand you. The setup page links to each
provider's page for creating a **key**, a long password that lets your Ghost use
that service. Make a free account, choose "Create API key", copy it, and paste it
back. You pay the provider directly for what you use; everyday chatting is usually inexpensive.
Not sure which? Choose DeepSeek. Or pick **On this device** to run entirely on the
Pod (private and offline, slower and less capable). A wrong key is refused on the
spot, and nothing is half-configured if you have to try again.

## 5. Meet Ghost

Give it your name and a name of its own, and say which city you are in (optional;
it lets Ghost answer "what's the weather?" without asking, and sets reminders in
your time zone). Choose an owner password and finish.

## 6. Bring your phone

Install the Ghost app and, on the Pod, ask for a pairing code: run `ghost pair`
(or use **Devices** in the console). Scan the QR in the app. It works once and
expires in five minutes.

Turn on notifications when the app asks. That is how Ghost reaches you when the
app is closed: reminders, questions, and anything it thinks you should know.

## Building your own Pod

```bash
sudo apt install -y git make golang-go
git clone https://github.com/ianclemence/ghost.git
cd ghost
sudo make install-ghost
sudo reboot
```

`make install-ghost` also installs what Ghost works with when it is missing:
ffmpeg, a browser (Chromium), pandoc and poppler, for voice, browsing, documents
and motion videos.

Then follow steps 2 to 6. To give the Pod a fixed setup code (for a batch you are
preparing), write it to `ghost-setup-code` on the boot partition before first
boot: 6 to 24 letters, digits or dashes.

## Sensors

If you attach a temperature and humidity sensor (for example a BME280 on I2C, or
a DHT22), Ghost feels the room. Enable the sensor's kernel driver in
`/boot/firmware/config.txt` (for a BME280, `dtparam=i2c_arm=on` and
`dtoverlay=i2c-sensor,bme280`), reboot, and ask "what's the temperature in
here?". Ghost also tells you unprompted when the Pod is running hot or the room
gets unusually hot, cold, damp or dry. With no sensor it says so plainly and
reports the Pod's own temperature.

## When something goes wrong

Ghost tells you, without being asked, when it cannot do something it should be
able to: the model provider's balance ran out, a key was rejected, storage is
nearly full, the Pod is overheating, or a newer version is available. It also
tells you if something on your network keeps trying to get in without valid
credentials. Each of these is said once, not repeated.
