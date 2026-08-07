import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const CACHE_TTL_MS = 60_000;

let cachedSnapshot;
let cachedAt = 0;

function workflowStatus(issue) {
    if (issue.state === "CLOSED") {
        return "Closed";
    }

    const labels = issue.labels.map((label) => label.name.toLowerCase());
    if (labels.some((label) => label === "blocked" || label.includes("status: blocked"))) {
        return "Blocked";
    }
    if (labels.some((label) => label === "in review" || label.includes("status: review"))) {
        return "In review";
    }
    if (
        labels.some(
            (label) =>
                label === "wip" ||
                label === "in progress" ||
                label.includes("status: in progress"),
        )
    ) {
        return "In progress";
    }
    return "Open";
}

async function runGh(args, cwd) {
    try {
        const { stdout } = await execFileAsync("gh", args, {
            cwd,
            encoding: "utf8",
            maxBuffer: 20 * 1024 * 1024,
        });
        return stdout.trim();
    } catch (error) {
        const detail = error?.stderr?.trim() || error?.message || String(error);
        throw new Error(`Unable to load GitHub issues: ${detail}`);
    }
}

export function invalidateIssueSnapshot() {
    cachedSnapshot = undefined;
    cachedAt = 0;
}

export async function getIssueSnapshot(cwd) {
    if (cachedSnapshot && Date.now() - cachedAt < CACHE_TTL_MS) {
        return cachedSnapshot;
    }

    const [repositoryOutput, issuesOutput] = await Promise.all([
        runGh(["repo", "view", "--json", "nameWithOwner,url"], cwd),
        runGh(
            [
                "issue",
                "list",
                "--state",
                "all",
                "--limit",
                "10000",
                "--json",
                "number,title,state,labels,assignees,author,updatedAt,url,milestone",
            ],
            cwd,
        ),
    ]);

    const repository = JSON.parse(repositoryOutput);
    const issues = JSON.parse(issuesOutput).map((issue) => ({
        ...issue,
        workflowStatus: workflowStatus(issue),
    }));
    const statusCount = (status) =>
        issues.filter((issue) => issue.workflowStatus === status).length;

    cachedSnapshot = {
        repository,
        fetchedAt: new Date().toISOString(),
        summary: {
            total: issues.length,
            open: issues.filter((issue) => issue.state === "OPEN").length,
            closed: issues.filter((issue) => issue.state === "CLOSED").length,
            inProgress: statusCount("In progress"),
            blocked: statusCount("Blocked"),
        },
        issues,
    };
    cachedAt = Date.now();
    return cachedSnapshot;
}
