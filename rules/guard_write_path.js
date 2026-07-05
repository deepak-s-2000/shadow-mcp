// Pre-hook example: only allows filesystem writes/edits/moves/directory
// creation under rules/sandbox/, rejecting anything else. A simple example of
// a path-based write guard - swap the allowed prefix for whatever your real
// sandbox boundary should be.
function onCall(input) {
    var args = input.arguments || {};
    var paths = [];
    if (typeof args.path === "string") paths.push(args.path);
    if (typeof args.destination === "string") paths.push(args.destination);
    if (typeof args.source === "string") paths.push(args.source);

    for (var i = 0; i < paths.length; i++) {
        var p = paths[i].replace(/\\/g, "/");
        if (p.indexOf("/rules/sandbox/") === -1 && p.indexOf("rules/sandbox/") !== 0) {
            return { action: "reject", reason: "writes are only allowed under rules/sandbox/ (guard-write-path rule)" };
        }
    }
    return { action: "continue", arguments: args };
}
