import json
import re
import sys

# Matches common secret-looking tokens in tool output text so they never
# reach the LLM in the clear. Extend this pattern list for your own secrets.
SECRET_PATTERN = re.compile(r"\b(SECRET|sk-|ghp_|xox[baprs]-)\w*")


def main():
    data = json.load(sys.stdin)
    result = data.get("result")

    if result and result.get("content"):
        for item in result["content"]:
            if "text" in item:
                item["text"] = SECRET_PATTERN.sub("[REDACTED]", item["text"])

    json.dump({"action": "continue", "result": result}, sys.stdout)


if __name__ == "__main__":
    main()
