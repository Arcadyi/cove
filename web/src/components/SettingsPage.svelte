<script lang="ts">
  import { onMount } from "svelte";
  import { settings } from "$lib/stores/settings";
  import type { Settings } from "$lib/types/settings";
  import { Button } from "$lib/components/ui/button";
  import { Label } from "$lib/components/ui/label";
  import { Switch } from "$lib/components/ui/switch/index.js";
  import { Slider } from "$lib/components/ui/slider/index.js";
  import { Separator } from "$lib/components/ui/separator/index.js";
  import * as Select from "$lib/components/ui/select/index.js";
  import * as Tabs from "$lib/components/ui/tabs/index.js";
  import { STREAM_SELECTION_MODES } from "$lib/streamSelection";
  import { DISCOVERY_ALGORITHMS } from "$lib/discoveryAlgorithms";
  import { api } from "$lib/api";
  import type { AddonEntry } from "$lib/types/addons";
  import {
    KindProvider,
    KindTimestamps,
    SourceOfficial,
  } from "$lib/types/addons";
  import { Badge } from "$lib/components/ui/badge/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { Trash2, Plus } from "lucide-svelte";

  let draft = $state<Settings | null>(null);
  let saved = $state(false);
  let saveTimer: ReturnType<typeof setTimeout>;

  onMount(async () => {
    await settings.load();
    const unsub = settings.subscribe((v) => {
      if (!draft) draft = { ...v };
    });
    unsub();
    loadAddons();
  });

  function patch<K extends keyof Settings>(key: K, value: Settings[K]) {
    if (!draft) return;
    draft = { ...draft, [key]: value };
  }

  function handleSave() {
    if (!draft) return;
    settings.save(draft);
    saved = true;
    clearTimeout(saveTimer);
    saveTimer = setTimeout(() => (saved = false), 2000);
  }

  function handleReset() {
    draft = null;
    settings.load().then(() => {
      const unsub = settings.subscribe((v) => {
        draft = { ...v };
      });
      unsub();
    });
  }

  // ── Addon management ─────────────────────────────────────────────────────────
  let addons = $state<AddonEntry[]>([]);
  let addAddonUrl = $state("");
  let addAddonError = $state<string | null>(null);
  let addAddonLoading = $state(false);

  async function loadAddons() {
    addons = await api.getAddons();
  }

  async function handleAddAddon() {
    if (!addAddonUrl.trim()) return;
    addAddonLoading = true;
    addAddonError = null;
    try {
      const entry = await api.addAddon(addAddonUrl.trim());
      addons = [...addons.filter((a) => a.id !== entry.id), entry];
      addAddonUrl = "";
    } catch (e) {
      addAddonError = e instanceof Error ? e.message : "Failed to add addon";
    } finally {
      addAddonLoading = false;
    }
  }

  async function handleToggleAddon(addon: AddonEntry) {
    await api.toggleAddon(addon.id, !addon.enabled, addon.url);
    addons = addons.map((a) =>
      a.id === addon.id && a.url === addon.url
        ? { ...a, enabled: !a.enabled }
        : a,
    );
  }

  async function handleRemoveAddon(addon: AddonEntry) {
    await api.removeAddon(addon.id, addon.url);
    addons = addons.filter((a) => !(a.id === addon.id && a.url === addon.url));
  }

  // ── Discovery algorithm ───────────────────────────────────────────────────────
  let testingAlgorithm = $state(false);
  let algorithmTestResult = $state<{ ok: boolean; error?: string } | null>(
    null,
  );

  async function handleTestAlgorithm() {
    if (!draft?.customAlgorithmUrl.trim()) return;
    testingAlgorithm = true;
    algorithmTestResult = null;
    try {
      algorithmTestResult = await api.testDiscoveryAlgorithm(
        draft.customAlgorithmUrl.trim(),
      );
    } catch (e) {
      algorithmTestResult = {
        ok: false,
        error: e instanceof Error ? e.message : "Test failed",
      };
    } finally {
      testingAlgorithm = false;
    }
  }

  const LANGUAGES = [
    { value: "en", label: "English" },
    { value: "es", label: "Spanish" },
    { value: "fr", label: "French" },
    { value: "de", label: "German" },
    { value: "pt", label: "Portuguese" },
    { value: "it", label: "Italian" },
    { value: "ja", label: "Japanese" },
    { value: "ko", label: "Korean" },
    { value: "zh", label: "Chinese" },
    { value: "ar", label: "Arabic" },
    { value: "ru", label: "Russian" },
  ];

  function langLabel(value: string) {
    return LANGUAGES.find((l) => l.value === value)?.label ?? value;
  }

  let testingSpeed = $state(false);
  let speedTestError = $state<string | null>(null);

  async function runSpeedTest() {
    if (!draft) return;
    testingSpeed = true;
    speedTestError = null;
    try {
      const start = performance.now();
      const res = await fetch(api.speedtestUrl(), {
        cache: "no-store",
      });
      const blob = await res.blob();
      const seconds = (performance.now() - start) / 1000;
      const mbps = (blob.size * 8) / 1_000_000 / seconds;
      patch("measuredBandwidthMbps", Math.round(mbps * 10) / 10);
    } catch {
      speedTestError = "Speed test failed — check your connection.";
    } finally {
      testingSpeed = false;
    }
  }
</script>

<div class="mx-auto max-w-2xl space-y-6 p-6 pt-18 pb-16">
  <div class="flex items-center justify-between">
    <h1 class="text-2xl font-semibold tracking-tight">Settings</h1>
    <div class="flex gap-2">
      <Button variant="outline" onclick={handleReset}>Reset</Button>
      <Button onclick={handleSave}>{saved ? "Saved ✓" : "Save"}</Button>
    </div>
  </div>

  {#if draft}
    <Tabs.Root value="playback">
      <Tabs.List class="w-full">
        <Tabs.Trigger value="playback">Playback</Tabs.Trigger>
        <Tabs.Trigger value="streaming">Streaming</Tabs.Trigger>
        <Tabs.Trigger value="subtitles">Subtitles & Audio</Tabs.Trigger>
        <Tabs.Trigger value="interface">Interface</Tabs.Trigger>
        <Tabs.Trigger value="addons">Addons</Tabs.Trigger>
      </Tabs.List>

      <!-- ── Playback ── -->
      <Tabs.Content value="playback" class="mt-4 space-y-1">
        <div class="flex items-center justify-between py-3">
          <div>
            <Label for="open-muted" class="text-sm font-medium"
              >Open videos muted</Label
            >
            <p class="text-xs text-muted-foreground">
              Start every video with audio muted.
            </p>
          </div>
          <Switch
            id="open-muted"
            checked={draft.openOnMute}
            onCheckedChange={(v) => patch("openOnMute", v)}
          />
        </div>
        <Separator />

        <div class="flex items-center justify-between py-3">
          <div>
            <Label class="text-sm font-medium">Default volume</Label>
            <p class="text-xs text-muted-foreground">Initial volume level.</p>
          </div>
          <div class="flex items-center gap-3">
            <Slider
              type="multiple"
              value={[draft.defaultVolume * 100]}
              min={0}
              max={100}
              step={1}
              class="w-32"
              onValueChange={([v]) => patch("defaultVolume", v / 100)}
            />
            <span
              class="w-9 text-right text-sm text-muted-foreground tabular-nums"
            >
              {Math.round(draft.defaultVolume * 100)}%
            </span>
          </div>
        </div>
        <Separator />

        <div class="flex items-center justify-between py-3">
          <div>
            <Label for="autoplay" class="text-sm font-medium"
              >Autoplay next episode</Label
            >
            <p class="text-xs text-muted-foreground">
              Automatically start the next episode when one finishes.
            </p>
          </div>
          <Switch
            id="autoplay"
            checked={draft.autoPlay}
            onCheckedChange={(v) => patch("autoPlay", v)}
          />
        </div>
        <Separator />

        <div class="flex items-center justify-between py-3">
          <div>
            <Label for="remember-pos" class="text-sm font-medium"
              >Remember position</Label
            >
            <p class="text-xs text-muted-foreground">
              Resume from where you left off.
            </p>
          </div>
          <Switch
            id="remember-pos"
            checked={draft.rememberPosition}
            onCheckedChange={(v) => patch("rememberPosition", v)}
          />
        </div>
        <Separator />

        <div class="py-3">
          <Label class="text-sm font-medium">Auto-skip segments</Label>
          <p class="mb-3 text-xs text-muted-foreground">
            Automatically skip segments when timestamps are available via
            IntroDB. A skip button always appears when inside a segment.
          </p>
          <div class="space-y-2">
            <div class="flex items-center justify-between">
              <Label for="skip-intro" class="text-sm text-muted-foreground"
                >Skip intro</Label
              >
              <Switch
                id="skip-intro"
                checked={draft.autoSkipIntro}
                onCheckedChange={(v) => patch("autoSkipIntro", v)}
              />
            </div>
            <div class="flex items-center justify-between">
              <Label for="skip-recap" class="text-sm text-muted-foreground"
                >Skip recap</Label
              >
              <Switch
                id="skip-recap"
                checked={draft.autoSkipRecap}
                onCheckedChange={(v) => patch("autoSkipRecap", v)}
              />
            </div>
            <div class="flex items-center justify-between">
              <Label for="skip-credits" class="text-sm text-muted-foreground"
                >Skip credits</Label
              >
              <Switch
                id="skip-credits"
                checked={draft.autoSkipCredits}
                onCheckedChange={(v) => patch("autoSkipCredits", v)}
              />
            </div>
            <div class="flex items-center justify-between">
              <Label for="skip-preview" class="text-sm text-muted-foreground"
                >Skip preview</Label
              >
              <Switch
                id="skip-preview"
                checked={draft.autoSkipPreview}
                onCheckedChange={(v) => patch("autoSkipPreview", v)}
              />
            </div>
          </div>
        </div>
      </Tabs.Content>

      <!-- ── Streaming ── -->
      <Tabs.Content value="streaming" class="mt-4 space-y-1">
        <div class="flex items-center justify-between py-3">
          <div>
            <Label for="auto-select-stream" class="text-sm font-medium"
              >Automatically pick best stream</Label
            >
            <p class="text-xs text-muted-foreground">
              Skip the stream list — start playing immediately when you press
              Watch.
            </p>
          </div>
          <Switch
            id="auto-select-stream"
            checked={draft.autoSelectStream}
            onCheckedChange={(v) => patch("autoSelectStream", v)}
          />
        </div>
        <Separator />

        <div class="flex items-center justify-between py-3">
          <div class="pr-4">
            <Label class="text-sm font-medium">Selection strategy</Label>
            <p class="text-xs text-muted-foreground">
              {STREAM_SELECTION_MODES.find(
                (m) => m.value === draft.streamSelectionMode,
              )?.description ?? ""}
            </p>
          </div>
          <Select.Root type="single" bind:value={draft.streamSelectionMode}>
            <Select.Trigger class="w-56 shrink-0">
              {STREAM_SELECTION_MODES.find(
                (m) => m.value === draft.streamSelectionMode,
              )?.label ?? "Choose…"}
            </Select.Trigger>
            <Select.Content>
              {#each STREAM_SELECTION_MODES as m (m.value)}
                <Select.Item value={m.value}>{m.label}</Select.Item>
              {/each}
            </Select.Content>
          </Select.Root>
        </div>
        <Separator />

        <div class="flex items-center justify-between py-3">
          <div class="pr-4">
            <Label class="text-sm font-medium">Connection speed</Label>
            {#if draft.measuredBandwidthMbps > 0}
              <p class="text-xs text-muted-foreground">
                Last measured at {draft.measuredBandwidthMbps} Mbps. Used by "Match
                My Internet Speed".
              </p>
            {:else}
              <p class="text-xs text-muted-foreground">
                Not measured yet — needed for "Match My Internet Speed".
              </p>
            {/if}
            {#if speedTestError}
              <p class="text-xs text-red-500">{speedTestError}</p>
            {/if}
          </div>
          <Button
            variant="outline"
            size="sm"
            class="shrink-0"
            onclick={runSpeedTest}
            disabled={testingSpeed}
          >
            {testingSpeed ? "Testing…" : "Test My Speed"}
          </Button>
        </div>
      </Tabs.Content>

      <!-- ── Subtitles & Audio ── -->
      <Tabs.Content value="subtitles" class="mt-4 space-y-1">
        <div class="flex items-center justify-between py-3">
          <div>
            <Label for="subs-enabled" class="text-sm font-medium"
              >Enable subtitles by default</Label
            >
            <p class="text-xs text-muted-foreground">
              Show subtitles automatically when available.
            </p>
          </div>
          <Switch
            id="subs-enabled"
            checked={draft.subtitlesEnabled}
            onCheckedChange={(v) => patch("subtitlesEnabled", v)}
          />
        </div>
        <Separator />

        <div class="flex items-center justify-between py-3">
          <div>
            <Label class="text-sm font-medium"
              >Preferred subtitle language</Label
            >
            <p class="text-xs text-muted-foreground">
              Auto-select this language when subtitles are available.
            </p>
          </div>
          <Select.Root type="single" bind:value={draft.defaultSubtitleLang}>
            <Select.Trigger class="w-36"
              >{langLabel(draft.defaultSubtitleLang)}</Select.Trigger
            >
            <Select.Content>
              {#each LANGUAGES as l}
                <Select.Item value={l.value}>{l.label}</Select.Item>
              {/each}
            </Select.Content>
          </Select.Root>
        </div>
        <Separator />

        <div class="flex items-center justify-between py-3">
          <div>
            <Label class="text-sm font-medium">Preferred audio language</Label>
            <p class="text-xs text-muted-foreground">
              Auto-select this audio track when multiple are available.
            </p>
          </div>
          <Select.Root type="single" bind:value={draft.defaultAudioLang}>
            <Select.Trigger class="w-36"
              >{langLabel(draft.defaultAudioLang)}</Select.Trigger
            >
            <Select.Content>
              {#each LANGUAGES as l}
                <Select.Item value={l.value}>{l.label}</Select.Item>
              {/each}
            </Select.Content>
          </Select.Root>
        </div>
      </Tabs.Content>

      <!-- ── Interface ── -->
      <Tabs.Content value="interface" class="mt-4 space-y-1">
        <div class="flex items-center justify-between py-3">
          <div>
            <Label for="stream-details" class="text-sm font-medium"
              >Show stream details</Label
            >
            <p class="text-xs text-muted-foreground">
              Display codec, resolution, and size badges on the stream list.
            </p>
          </div>
          <Switch
            id="stream-details"
            checked={draft.showStreamDetails}
            onCheckedChange={(v) => patch("showStreamDetails", v)}
          />
        </div>
        <div class="flex items-center justify-between py-3">
          <div>
            <Label for="thumbnail-previes" class="text-sm font-medium"
              >Hide Spoilers</Label
            >
            <p class="text-xs text-muted-foreground">
              Hide not-seen episode thumbnails, descriptions, and episode names;
            </p>
          </div>
          <Switch
            id="stream-details"
            checked={draft.hideSpoilers}
            onCheckedChange={(v) => patch("hideSpoilers", v)}
          />
        </div>

        <Separator class="my-2" />

        <div class="flex items-center justify-between py-3">
          <div class="pr-4">
            <Label class="text-sm font-medium">Discovery algorithm</Label>
            <p class="text-xs text-muted-foreground">
              {DISCOVERY_ALGORITHMS.find(
                (a) => a.value === draft.discoveryAlgorithm,
              )?.description ?? ""}
            </p>
          </div>
          <Select.Root type="single" bind:value={draft.discoveryAlgorithm}>
            <Select.Trigger class="w-56 shrink-0">
              {DISCOVERY_ALGORITHMS.find(
                (a) => a.value === draft.discoveryAlgorithm,
              )?.label ?? "Choose…"}
            </Select.Trigger>
            <Select.Content>
              {#each DISCOVERY_ALGORITHMS as a (a.value)}
                <Select.Item value={a.value}>{a.label}</Select.Item>
              {/each}
            </Select.Content>
          </Select.Root>
        </div>

        {#if draft.discoveryAlgorithm === "custom"}
          <div class="rounded-lg border border-border p-4">
            <Label class="mb-2 block text-sm font-medium"
              >Custom algorithm URL</Label
            >
            <p class="mb-3 text-xs text-muted-foreground">
              Cove POSTs your taste profile and a pre-filtered candidate list to
              this URL and expects relevance scores back. Falls back to Cove
              Smart if the endpoint is unreachable or errors.
            </p>
            <div class="flex gap-2">
              <Input
                type="url"
                placeholder="https://..."
                bind:value={draft.customAlgorithmUrl}
                class="flex-1"
              />
              <Button
                variant="outline"
                onclick={handleTestAlgorithm}
                disabled={testingAlgorithm || !draft.customAlgorithmUrl.trim()}
                size="sm"
              >
                {testingAlgorithm ? "Testing…" : "Test connection"}
              </Button>
            </div>
            {#if algorithmTestResult}
              <p
                class="mt-2 text-xs {algorithmTestResult.ok
                  ? 'text-green-500'
                  : 'text-red-500'}"
              >
                {algorithmTestResult.ok
                  ? "Connected successfully."
                  : `Failed: ${algorithmTestResult.error}`}
              </p>
            {/if}
          </div>
        {/if}
      </Tabs.Content>

      <!-- ── Addons ── -->
      <Tabs.Content value="addons" class="mt-4 space-y-4">
        <!-- Add new addon -->
        <div class="rounded-lg border border-border p-4">
          <Label class="mb-2 block text-sm font-medium">Add Stremio addon</Label
          >
          <p class="mb-3 text-xs text-muted-foreground">
            Paste a Stremio-compatible addon manifest URL.
          </p>
          <div class="flex gap-2">
            <Input
              type="url"
              placeholder="https://..."
              bind:value={addAddonUrl}
              class="flex-1"
              onkeydown={(e) => e.key === "Enter" && handleAddAddon()}
            />
            <Button
              onclick={handleAddAddon}
              disabled={addAddonLoading || !addAddonUrl.trim()}
              size="sm"
            >
              <Plus class="mr-1 size-4" />
              {addAddonLoading ? "Adding…" : "Add"}
            </Button>
          </div>
          {#if addAddonError}
            <p class="mt-2 text-xs text-red-500">{addAddonError}</p>
          {/if}
        </div>

        <!-- Addon list -->
        <div class="space-y-2">
          {#each addons as addon (addon.id)}
            <div
              class="flex items-center gap-3 rounded-lg border border-border bg-secondary/30 p-3"
            >
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                  <span class="text-sm font-medium"
                    >{addon.manifest.name ||
                      addon.url ||
                      addon.id ||
                      "Unknown addon"}</span
                  >
                  <Badge
                    variant="outline"
                    class={addon.kind === KindProvider
                      ? "border-blue-500/30 bg-blue-500/20 text-blue-400"
                      : addon.kind === KindTimestamps
                        ? "border-amber-500/30 bg-amber-500/20 text-amber-400"
                        : "border-purple-500/30 bg-purple-500/20 text-purple-400"}
                  >
                    {addon.kind === KindProvider
                      ? "Provider"
                      : addon.kind === KindTimestamps
                        ? "Timestamps"
                        : "Subtitles"}
                  </Badge>
                  {#if addon.source === SourceOfficial}
                    <Badge
                      variant="outline"
                      class="border-green-500/30 bg-green-500/20 text-green-400"
                      >Built-in</Badge
                    >
                  {/if}
                </div>
                {#if addon.manifest.description}
                  <p class="mt-0.5 text-xs text-muted-foreground">
                    {addon.manifest.description}
                  </p>
                {/if}
              </div>

              <!-- Toggle -->
              <Switch
                checked={addon.enabled}
                onCheckedChange={() => handleToggleAddon(addon)}
                class="shrink-0"
              />

              <!-- Remove (stremio only) -->
              {#if addon.source !== SourceOfficial}
                <Button
                  variant="ghost"
                  size="icon"
                  class="shrink-0 text-muted-foreground hover:text-destructive"
                  onclick={() => handleRemoveAddon(addon)}
                  title="Remove"
                >
                  <Trash2 class="size-4" />
                </Button>
              {/if}
            </div>
          {:else}
            <p class="py-4 text-center text-sm text-muted-foreground">
              No addons yet. Add a Stremio-compatible addon above.
            </p>
          {/each}
        </div>
      </Tabs.Content>
    </Tabs.Root>
  {:else}
    <p class="text-muted-foreground">Loading settings…</p>
  {/if}
</div>
