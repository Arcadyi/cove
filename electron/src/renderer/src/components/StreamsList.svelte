<script lang="ts">
  import { getMaxQuality, inferQuality } from "$lib/utils";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";
  import type { Stream } from "$lib/types/addons";
  import * as Select from "$lib/components/ui/select/index.js";
  import { ListFilter, Play, Settings2 } from "lucide-svelte";
  import { api } from "$lib/api";

  let loadingStreams = $state(false);
  let sortMode = $state<"seeders" | "size">("seeders");
  let qualityFilter = $state("all");

  let {
    media,
    onPlayStream,
    maxQuality = $bindable<string | null>(),
  } = $props();

  type StreamView = Stream & {
    seeders: number;
    sizeBytes: number;
    quality: string | null;
  };

  let streams: Stream[] = $state([]);

  $effect(() => {
    loadingStreams = true;
    api.getStreams(media.id).then((res) => {
      streams = res;
      loadingStreams = false;
      maxQuality = getMaxQuality(streams);
    });
  });

  const availableQualities = $derived.by(() => {
    const qualities = [
      ...new Set(streams.map((s) => inferQuality(s)).filter(Boolean)),
    ];

    qualities.sort((a, b) => {
      const order = ["4k dv", "4k hdr", "4k", "1080p", "720p"];

      return order.indexOf(a!) - order.indexOf(b!);
    });

    return ["all", ...qualities];
  });

  function getSeeders(stream: Stream): number {
    const match = stream.title.match(/👤\s*(\d+)/);
    return match ? Number(match[1]) : 0;
  }

  function getSizeBytes(stream: Stream): number {
    const match = stream.title.match(/💾\s*([\d.]+)\s*(TB|GB|MB)/i);
    if (!match) return 0;

    const value = Number(match[1]);
    const unit = match[2].toUpperCase();

    switch (unit) {
      case "TB":
        return value * 1024 ** 4;
      case "GB":
        return value * 1024 ** 3;
      case "MB":
        return value * 1024 ** 2;
      default:
        return 0;
    }
  }
  const filteredStreams = $derived.by(() => {
    const list: StreamView[] = streams.map((stream) => ({
      ...stream,
      seeders: getSeeders(stream),
      sizeBytes: getSizeBytes(stream),
      quality: inferQuality(stream),
    }));

    const filtered = list.filter((stream) => {
      if (qualityFilter === "all") return true;
      return stream.quality === qualityFilter;
    });

    filtered.sort((a, b) => {
      if (sortMode === "seeders") {
        return b.seeders - a.seeders;
      }

      return b.sizeBytes - a.sizeBytes;
    });

    return filtered;
  });
</script>

<ScrollArea
  class="h-full w-[35%] border-l border-border bg-background/60 backdrop-blur-xl"
>
  <div class="flex h-full flex-col">
    <div class="flex-none space-y-3 border-b border-border p-5">
      <h3 class="text-lg font-semibold">Available Streams</h3>

      <div class="grid grid-cols-2 gap-2">
        <!-- Quality -->
        <Select.Root type="single" bind:value={qualityFilter}>
          <Select.Trigger class="flex w-full">
            <span class="flex flex-row items-center justify-center gap-1">
              <Settings2 />
              {qualityFilter.toUpperCase()}
            </span>
          </Select.Trigger>
          <Select.Content>
            <Select.Group>
              {#each availableQualities as quality (quality)}
                <Select.Item value={quality} label={quality.toUpperCase()} />
              {/each}
            </Select.Group>
          </Select.Content>
        </Select.Root>
        <!-- Sort -->
        <Select.Root type="single" bind:value={sortMode}>
          <Select.Trigger class="flex w-full">
            <span class="flex flex-row items-center justify-center gap-1">
              <ListFilter />
              {sortMode.toUpperCase()}
            </span>
          </Select.Trigger>
          <Select.Content>
            <Select.Group>
              <Select.Item value="seeders" label="Seeders" />
              <Select.Item value="size" label="Size" />
            </Select.Group>
          </Select.Content>
        </Select.Root>
      </div>
    </div>

    <div class="flex-1 overflow-y-auto p-4">
      {#if loadingStreams}
        <div class="flex h-full items-center justify-center">
          <span class="animate-pulse text-sm text-muted-foreground">
            Finding streams...
          </span>
        </div>
      {:else if streams.length === 0}
        <div class="flex h-full items-center justify-center">
          <span class="text-sm text-muted-foreground"> No streams found. </span>
        </div>
      {:else if filteredStreams.length === 0}
        <div class="flex h-full items-center justify-center">
          <span class="text-sm text-muted-foreground">
            No streams match this filter.
          </span>
        </div>
      {:else}
        <div class="flex flex-col gap-3">
          {#each filteredStreams as stream (stream)}
            <button
              class="group flex w-full flex-col gap-1 rounded-lg border border-border/50 bg-secondary/50 p-3 text-left transition-colors hover:border-border hover:bg-secondary"
              onclick={() => onPlayStream(stream)}
            >
              <span class="flex items-center justify-between gap-2">
                <span class="text-sm font-medium text-foreground">
                  {stream.name}
                </span>
                <Play
                  class="size-3 text-foreground opacity-0 transition-opacity group-hover:opacity-100"
                />
              </span>

              <span
                class="line-clamp-2 text-xs whitespace-pre-line text-muted-foreground"
              >
                {stream.title}
              </span>

              <span
                class="mt-1 flex flex-wrap gap-1.5 text-[11px] text-muted-foreground"
              >
                <span class="rounded bg-background/70 px-1.5 py-0.5">
                  👤 {getSeeders(stream)}
                </span>
                <span class="rounded bg-background/70 px-1.5 py-0.5">
                  💾 {getSizeBytes(stream) / 1024 ** 3 >= 1
                    ? `${(getSizeBytes(stream) / 1024 ** 3).toFixed(2)} GB`
                    : `${(getSizeBytes(stream) / 1024 ** 2).toFixed(0)} MB`}
                </span>
                <span class="rounded bg-background/70 px-1.5 py-0.5">
                  {inferQuality(stream)}
                </span>
              </span>
            </button>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</ScrollArea>
