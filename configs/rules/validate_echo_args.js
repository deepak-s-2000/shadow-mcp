// Pre-hook example: rejects a call before it ever reaches the downstream
// server. Replace this check with whatever validation/governance your team
// needs (e.g. blocking destructive args, requiring a ticket ID, etc).
function onCall(input) {
    var args = input.arguments || {};
    if (typeof args.message === "string" && args.message.indexOf("forbidden") !== -1) {
        return { action: "reject", reason: "message contains a forbidden word" };
    }
    return { action: "continue", arguments: args };
}
