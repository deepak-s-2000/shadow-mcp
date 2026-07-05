import json
import re
import sys

# Post-hook example: redacts secret-looking tokens from file read results,
# so they never reach the LLM in the clear. Recurses through the whole result
# (not just content[].text) because tool results can carry the same data
# twice - e.g. the filesystem server also mirrors text into
# structuredContent - and both copies need scrubbing.
SECRET_PATTERN = re.compile(r"\b(SECRET|sk-|ghp_|xox[baprs]-)\w*")


def redact(value):
    if isinstance(value, str):
        return SECRET_PATTERN.sub("[REDACTED]", value)
    if isinstance(value, list):
        return [redact(v) for v in value]
    if isinstance(value, dict):
        return {k: redact(v) for k, v in value.items()}
    return value


def main():
    data = json.load(sys.stdin)
    result = data.get("result")

    if result:
        result = redact(result)

    json.dump({"action": "continue", "result": result}, sys.stdout)


if __name__ == "__main__":
    main()
