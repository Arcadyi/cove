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
  import { api } from "$lib/api";

  let draft = $state<Settings | null>(null);
  let saved = $state(false);
  let saveTimer: ReturnType<typeof setTimeout>;

  onMount(async () => {
    await settings.load();
    const unsub = settings.subscribe((v) => {
      if (!draft) draft = { ...v };
    });
    unsub();
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

  const PROVIDERS = [
    { value: "torrentio", label: "Torrentio" },
    { value: "debrid", label: "Debrid (Real-Debrid / AllDebrid)" },
  ];

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

  function providerLabel(value: string) {
    return PROVIDERS.find((p) => p.value === value)?.label ?? value;
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
      </Tabs.Content>

      <!-- ── Streaming ── -->
      <Tabs.Content value="streaming" class="mt-4 space-y-1">
        <div class="flex items-center justify-between py-3">
          <div>
            <Label class="text-sm font-medium">Default provider</Label>
            <p class="text-xs text-muted-foreground">
              Which addon to prefer when multiple streams are available.
            </p>
          </div>
          <Select.Root type="single" bind:value={draft.defaultProvider}>
            <Select.Trigger class="w-52"
            >{providerLabel(draft.defaultProvider)}</Select.Trigger
            >
            <Select.Content>
              {#each PROVIDERS as p}
                <Select.Item value={p.value}>{p.label}</Select.Item>
              {/each}
            </Select.Content>
          </Select.Root>
        </div>
        <Separator />

        <div class="flex items-center justify-between py-3">
          <div>
            <Label for="prefer-hls" class="text-sm font-medium"
            >Use HLS pipeline</Label
            >
            <p class="text-xs text-muted-foreground">
              Re-mux via ffmpeg before playing. Better seek support, higher CPU
              usage.
            </p>
          </div>
          <Switch
            id="prefer-hls"
            checked={draft.preferHLS}
            onCheckedChange={(v) => patch("preferHLS", v)}
          />
        </div>
        <Separator />

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
        <Separator />

        <div class="flex items-center justify-between py-3">
          <div>
            <Label class="text-sm font-medium">Subtitle size</Label>
            <p class="text-xs text-muted-foreground">
              Default text size for subtitles in the player.
            </p>
          </div>
          <div class="flex items-center gap-3">
            <Slider
              type="multiple"
              value={[draft.subtitleSize]}
              min={50}
              max={200}
              step={10}
              class="w-32"
              onValueChange={([v]) => patch("subtitleSize", v)}
            />
            <span
              class="w-9 text-right text-sm text-muted-foreground tabular-nums"
            >{draft.subtitleSize}%</span
            >
          </div>
        </div>
        <Separator />

        <div class="flex items-center justify-between py-3">
          <div>
            <Label class="text-sm font-medium">Subtitle position</Label>
            <p class="text-xs text-muted-foreground">
              Vertical placement from the bottom of the screen.
            </p>
          </div>
          <div class="flex items-center gap-3">
            <Slider
              type="multiple"
              value={[draft.subtitlePosition]}
              min={2}
              max={90}
              step={1}
              class="w-32"
              onValueChange={([v]) => patch("subtitlePosition", v)}
            />
            <span
              class="w-9 text-right text-sm text-muted-foreground tabular-nums"
            >{draft.subtitlePosition}%</span
            >
          </div>
        </div>
        <Separator />

        <div class="flex items-center justify-between py-3">
          <div>
            <Label for="subtitle-bg" class="text-sm font-medium"
            >Subtitle background</Label
            >
            <p class="text-xs text-muted-foreground">
              Show a dark box behind subtitle text for readability.
            </p>
          </div>
          <Switch
            id="subtitle-bg"
            checked={draft.subtitleBackground}
            onCheckedChange={(v) => patch("subtitleBackground", v)}
          />
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
      </Tabs.Content>
    </Tabs.Root>
  {:else}
    <p class="text-muted-foreground">Loading settings…</p>
  {/if}
</div>
