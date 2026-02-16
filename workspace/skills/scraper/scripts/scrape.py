import sys
import requests

def scrape(url, format_type="markdown"):
    # r.jina.ai
    api_url = f"https://r.jina.ai/{url}"
    headers = {}
    if format_type == "text":
        headers["Accept"] = "text/plain"
    
    try:
        response = requests.get(api_url, headers=headers)
        # Use utf-8 encoding to avoid printing errors on some terminals
        response.encoding = 'utf-8'
        print(response.text)
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python scrape.py <url> [markdown|text]")
    else:
        fmt = "markdown"
        if len(sys.argv) > 2:
            fmt = sys.argv[2]
        scrape(sys.argv[1], fmt)
