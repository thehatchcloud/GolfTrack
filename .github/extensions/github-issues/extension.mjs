import { createServer } from "node:http";
import { fileURLToPath } from "node:url";
import { createCanvas, joinSession } from "@github/copilot-sdk/extension";

import { getIssueSnapshot, invalidateIssueSnapshot } from "./github.mjs";
import { renderHtml } from "./renderer.mjs";

const servers = new Map();
const repositoryPath = fileURLToPath(new URL("../../..", import.meta.url));

function sendJson(res, status, body) {
    res.writeHead(status, {
        "Content-Type": "application/json; charset=utf-8",
        "Cache-Control": "no-store",
    });
    res.end(JSON.stringify(body));
}

function notifyClients() {
    for (const entry of servers.values()) {
        for (const response of entry.clients) {
            response.write("event: refresh\ndata: {}\n\n");
        }
    }
}

async function loadSnapshot(force = false) {
    if (force) {
        invalidateIssueSnapshot();
    }
    return getIssueSnapshot(repositoryPath);
}

async function startServer() {
    const clients = new Set();
    const server = createServer(async (req, res) => {
        const url = new URL(req.url ?? "/", "http://127.0.0.1");

        if (req.method === "GET" && url.pathname === "/") {
            res.writeHead(200, {
                "Content-Type": "text/html; charset=utf-8",
                "Cache-Control": "no-store",
            });
            res.end(renderHtml());
            return;
        }

        if (req.method === "GET" && url.pathname === "/api/issues") {
            try {
                sendJson(res, 200, await loadSnapshot(url.searchParams.get("refresh") === "1"));
            } catch (error) {
                sendJson(res, 502, {
                    error: error instanceof Error ? error.message : String(error),
                });
            }
            return;
        }

        if (req.method === "GET" && url.pathname === "/events") {
            res.writeHead(200, {
                "Content-Type": "text/event-stream",
                "Cache-Control": "no-cache",
                Connection: "keep-alive",
            });
            res.write(": connected\n\n");
            clients.add(res);
            req.on("close", () => clients.delete(res));
            return;
        }

        sendJson(res, 404, { error: "Not found" });
    });

    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    const port = typeof address === "object" && address ? address.port : 0;
    return { clients, server, url: `http://127.0.0.1:${port}/` };
}

const session = await joinSession({
    canvases: [
        createCanvas({
            id: "github-issues",
            displayName: "GitHub Issues",
            description: "Track every issue in the current repository by state, workflow status, assignee, and label.",
            inputSchema: {
                type: "object",
                additionalProperties: false,
            },
            actions: [
                {
                    name: "refresh",
                    description: "Refresh the issue dashboard from GitHub and return its current totals.",
                    handler: async () => {
                        const snapshot = await loadSnapshot(true);
                        notifyClients();
                        return {
                            repository: snapshot.repository,
                            total: snapshot.summary.total,
                            open: snapshot.summary.open,
                            closed: snapshot.summary.closed,
                            inProgress: snapshot.summary.inProgress,
                            blocked: snapshot.summary.blocked,
                        };
                    },
                },
            ],
            open: async (ctx) => {
                let entry = servers.get(ctx.instanceId);
                if (!entry) {
                    entry = await startServer();
                    servers.set(ctx.instanceId, entry);
                }
                return {
                    title: "GitHub Issues",
                    status: "Live repository data",
                    url: entry.url,
                };
            },
            onClose: async (ctx) => {
                const entry = servers.get(ctx.instanceId);
                if (entry) {
                    servers.delete(ctx.instanceId);
                    for (const response of entry.clients) {
                        response.end();
                    }
                    await new Promise((resolve) => entry.server.close(resolve));
                }
            },
        }),
    ],
});
