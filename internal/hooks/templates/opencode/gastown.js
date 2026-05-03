// Gas Town OpenCode plugin: hooks SessionStart/Compaction via events.
// Injects gt prime context into the system prompt via experimental.chat.system.transform.
//
// Compaction auto-cycling: After MAX_COMPACTIONS cycles, the plugin saves state
// (costs + handoff mail), kills the tmux session, and the daemon patrol detects
// the dead session and re-spawns the polecat with a fresh context window. This
// replaces Claude's native PreCompact → gt handoff --cycle hook chain for agents
// that cannot self-respawn from within their hook/plugin system.
export const GasTown = async ({ $, directory }) => {
  const role = (process.env.GT_ROLE || "").toLowerCase();
  const autonomousRoles = new Set(["polecat", "witness", "refinery", "deacon"]);
  let didInit = false;

  // Promise-based context loading ensures the system transform hook can
  // await the result even if session.created hasn't resolved yet.
  let primePromise = null;

  // Compaction tracking for session auto-cycle (replaces Claude's PreCompact hook).
  // After MAX_COMPACTIONS, the plugin signals the daemon to restart the session.
  // This prevents context quality degradation for non-Claude agents that lack
  // Claude's native session-cycling hook (gt handoff --cycle).
  const MAX_COMPACTIONS = 3;
  let compactionCount = 0;
  let cycleSignalled = false;

  const captureRun = async (cmd) => {
    try {
      return await $`/bin/sh -lc ${cmd}`.cwd(directory).text();
    } catch (err) {
      console.error(`[gastown] ${cmd} failed`, err?.message || err);
      return "";
    }
  };

  const loadPrime = async () => {
    let context = await captureRun("gt prime");
    if (autonomousRoles.has(role)) {
      const mail = await captureRun("gt mail check --inject");
      if (mail) {
        context += "\n" + mail;
      }
    }
    return context;
  };

  const signalSessionCycle = async () => {
    if (cycleSignalled) return;
    cycleSignalled = true;
    const sessionName = process.env.GT_SESSION_NAME || "";
    // Save state before cycling: record costs and send handoff mail so the
    // next session inherits context. Uses --auto (save-only, no respawn)
    // because opencode cannot self-respawn via hooks.
    await $`gt costs record`.cwd(directory).catch(() => {});
    await $`gt handoff --auto -s "OpenCode compaction cycle" -m "Compacted ${compactionCount} times — context snapshot for successor"`.cwd(directory).catch(() => {});

    if (sessionName) {
      // Kill the tmux session to trigger daemon-driven restart. The deacon/witness
      // patrol detects the dead session, reads the handoff mail, and re-spawns
      // the polecat with a fresh context window. This replaces Claude's native
      // session-cycling PreCompact hook (gt handoff --cycle) for non-Claude agents.
      await $`tmux kill-session -t ${sessionName}`.cwd(directory).catch(() => {});
      console.error(`[gastown] session cycle complete — killed ${sessionName} after ${compactionCount} compactions`);
    } else {
      console.error(`[gastown] session cycle signalled after ${compactionCount} compactions — waiting for daemon patrol`);
    }
  };

  return {
    event: async ({ event }) => {
      if (event?.type === "session.created") {
        if (didInit) return;
        didInit = true;
        compactionCount = 0;
        cycleSignalled = false;
        primePromise = loadPrime();
      }
      if (event?.type === "session.compacted") {
        compactionCount++;
        primePromise = loadPrime();
        if (compactionCount >= MAX_COMPACTIONS) {
          await signalSessionCycle();
        }
      }
      if (event?.type === "session.deleted") {
        const sessionID = event.properties?.info?.id;
        if (sessionID) {
          await $`gt costs record --session ${sessionID}`.catch(() => {});
        }
      }
    },
    "experimental.chat.system.transform": async (input, output) => {
      if (!primePromise) {
        primePromise = loadPrime();
      }
      const context = await primePromise;
      if (context) {
        output.system.push(context);
      } else {
        primePromise = null;
      }
    },
    "experimental.session.compacting": async ({ sessionID }, output) => {
      const roleDisplay = role || "unknown";
      const willCycle = compactionCount + 1 >= MAX_COMPACTIONS;
      output.context.push(`
## Gas Town Multi-Agent System

**After Compaction:** Run \`gt prime\` to restore full context.
**Check Hook:** \`gt hook\` - if work present, execute immediately (GUPP).
**Role:** ${roleDisplay}${willCycle ? `\n**Session Cycle:** Compaction limit reached (${compactionCount + 1}/${MAX_COMPACTIONS}). The daemon will restart this session after compaction to restore full context quality.` : ""}
`);
    },
  };
};
