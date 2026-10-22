"""Records the README demo: the app's web build at phone size, driven by
Playwright against a local gateway fed by the simulator.

  SOURCE=sim ./bin/ingest &                      # REDIS_URL set
  PORT=8097 ALLOWED_ORIGINS=localhost:8096 ./bin/gateway &
  (cd app && EXPO_PUBLIC_GATEWAY_URL=ws://localhost:8097/ws npx expo export --platform web --output-dir /tmp/webdist)
  (cd /tmp/webdist && python3 -m http.server 8096) &
  python3 scripts/record_demo.py                 # then ffmpeg the .webm to docs/demo.gif

Set CHROME to a Chrome binary, or leave it unset to use Playwright's own.
"""
import os
import re
import time

from playwright.sync_api import sync_playwright

APP = "http://localhost:8096"
OUT = os.environ.get("OUT", "/tmp/demo")
PHONE = {"width": 390, "height": 844}


def add(page, text):
    box = page.get_by_placeholder("Add a market, e.g. ETH or DOGE-USD")
    box.click()
    box.type(text, delay=90)
    box.press("Enter")
    time.sleep(0.8)


with sync_playwright() as p:
    chrome = os.environ.get("CHROME")
    browser = p.chromium.launch(executable_path=chrome or None, args=["--no-sandbox"])

    # Dark mode, recorded.
    ctx = browser.new_context(
        viewport=PHONE, device_scale_factor=2, color_scheme="dark",
        is_mobile=True, has_touch=True,
        record_video_dir=OUT, record_video_size=PHONE,
    )
    page = ctx.new_page()
    page.goto(APP)
    page.wait_for_selector('[aria-label^="BTC-USD"]')
    time.sleep(2.5)

    add(page, "doge")
    add(page, "avax")
    add(page, "LINK-USD")
    time.sleep(2.5)
    page.screenshot(path=f"{OUT}/watchlist-dark.png")

    # Set an alert just above the live price so it fires on camera.
    page.locator('[aria-label^="BTC-USD"]').click()
    time.sleep(0.8)
    label = page.locator('[aria-label^="BTC-USD"]').get_attribute("aria-label")
    shown = float(label.split(" ", 1)[1].replace(",", ""))
    target = round(shown * 1.0002, 2)
    field = page.get_by_placeholder("Target price")
    field.click()
    field.type(f"{target}", delay=70)
    time.sleep(0.4)
    page.screenshot(path=f"{OUT}/alert-sheet-dark.png")
    page.get_by_text("Set alert").click()
    time.sleep(0.6)
    page.get_by_label("Close").click()

    # Wait for the alert banner.
    try:
        page.get_by_text(re.compile(r"BTC-USD (rose|fell) to")).wait_for(timeout=60_000)
        time.sleep(0.4)
        page.screenshot(path=f"{OUT}/alert-fired-dark.png")
        time.sleep(3)
    except Exception as e:
        print("alert did not fire in time:", e)
    ctx.close()  # finalises the video

    # Light mode still.
    ctx = browser.new_context(viewport=PHONE, device_scale_factor=2, color_scheme="light", is_mobile=True)
    page = ctx.new_page()
    page.goto(APP)
    page.wait_for_selector('[aria-label^="BTC-USD"]')
    time.sleep(3)
    page.screenshot(path=f"{OUT}/watchlist-light.png")
    ctx.close()
    browser.close()
print("done")
