<script lang="ts">
  import { onMount } from "svelte";
  import { Download, RefreshCw, TriangleAlert } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button";

  // idle  → nothing shown (still checking, or no update — app runs normally)
  // available → update found, download starting (indeterminate bar)
  // downloading → download in progress (% bar)
  // downloaded → applied on restart (auto-triggers install)
  // error → download/install failed (escape hatch, don't trap the user)
  type Phase = "idle" | "available" | "downloading" | "downloaded" | "error";

  let phase = $state<Phase>("idle");
  let percent = $state(0);
  let version = $state("");
  let errorMessage = $state("");

  const pct = (p: number): string =>
    `${Math.max(0, Math.min(100, Math.round(p)))}%`;

  onMount(() => {
    const u = window.updates;
    if (!u) return; // not running under Electron (e.g. plain web preview)

    const offs = [
      u.on("update:available", (p) => {
        version = (p as { version?: string })?.version ?? "";
        percent = 0;
        phase = "available";
      }),
      u.on("update:progress", (p) => {
        percent = (p as { percent?: number })?.percent ?? 0;
        phase = "downloading";
      }),
      u.on("update:downloaded", (p) => {
        version = (p as { version?: string })?.version ?? version;
        percent = 100;
        phase = "downloaded";
        // Apply + restart. Brief delay so the "restarting" state is visible.
        setTimeout(() => u.install(), 1200);
      }),
      u.on("update:error", (p) => {
        errorMessage =
          (p as { message?: string })?.message ?? "Update failed.";
        phase = "error";
      }),
      u.on("update:none", () => {
        // Only clear if we weren't mid-update.
        if (phase !== "downloading" && phase !== "downloaded") phase = "idle";
      }),
      // "update:checking" is intentionally ignored — we never block during the
      // initial check, only once an update is actually found.
    ];

    // Main auto-checks a few seconds after launch; kick one now too so we react
    // as early as possible. No-op in dev (the check handler isn't registered).
    u.check?.().catch(() => {});

    return () => offs.forEach((off) => off?.());
  });

  function retry(): void {
    errorMessage = "";
    phase = "idle";
    window.updates?.check?.().catch(() => {});
  }

  function dismiss(): void {
    phase = "idle";
  }
</script>

{#if phase !== "idle"}
  <!-- Full-screen gate. The drag region keeps the frameless window movable. -->
  <div
    class="fixed inset-0 z-[100] flex flex-col items-center justify-center bg-background/95 backdrop-blur-md"
    style="-webkit-app-region: drag;"
  >
    <div
      class="flex w-[min(420px,90vw)] flex-col items-center gap-5 rounded-2xl border border-border bg-card p-8 text-center shadow-2xl"
      style="-webkit-app-region: no-drag;"
    >
      {#if phase === "error"}
        <span
          class="flex size-14 items-center justify-center rounded-full bg-destructive/10 text-destructive"
        >
          <TriangleAlert class="size-7" />
        </span>
        <div class="space-y-1">
          <h2 class="text-lg font-semibold">Update failed</h2>
          <p class="max-w-full truncate text-sm text-muted-foreground">
            {errorMessage}
          </p>
        </div>
        <div class="flex gap-2">
          <Button variant="outline" onclick={dismiss}>Continue anyway</Button>
          <Button onclick={retry}>Retry</Button>
        </div>
      {:else}
        <span
          class="flex size-14 items-center justify-center rounded-full bg-accent/10 text-accent"
        >
          {#if phase === "downloaded"}
            <RefreshCw class="size-7 animate-spin" />
          {:else}
            <Download class="size-7" />
          {/if}
        </span>

        <div class="space-y-1">
          <h2 class="text-lg font-semibold">
            {phase === "downloaded" ? "Restarting to update" : "Updating Cove"}
          </h2>
          <p class="text-sm text-muted-foreground">
            {#if phase === "downloaded"}
              The app will restart automatically.
            {:else if version}
              Downloading version {version}…
            {:else}
              Preparing update…
            {/if}
          </p>
        </div>

        <div class="w-full space-y-1.5">
          <div class="h-2 w-full overflow-hidden rounded-full bg-secondary">
            {#if phase === "available"}
              <!-- indeterminate until the first progress tick -->
              <div
                class="h-full w-1/3 animate-pulse rounded-full bg-accent"
              ></div>
            {:else}
              <div
                class="h-full rounded-full bg-accent transition-[width] duration-200"
                style="width: {pct(percent)}"
              ></div>
            {/if}
          </div>
          {#if phase === "downloading"}
            <p class="text-right text-xs text-muted-foreground">
              {pct(percent)}
            </p>
          {/if}
        </div>
      {/if}
    </div>

    <p class="mt-6 text-xs text-muted-foreground/70">
      Please keep the app open while it updates.
    </p>
  </div>
{/if}
