<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";
  import MediaCard from "./MediaCard.svelte";
  import { SvelteMap } from "svelte/reactivity";
  import { api } from "$lib/api";
  import { Button } from "$lib/components/ui/button";
  import { animate, splitText, stagger } from "animejs";
  import { tick } from "svelte";

  let {
    query = $bindable(""),
    loading = $bindable(false),
    onSelectMedia,
    onSuggested,
    onWatch,
  } = $props();

  let results: Media[] = $state([]);
  let keywords: { id: number; name: string }[] = $state([]);
  let qualityMap = new SvelteMap<number, string>();

  let resultsTextEl = $state<HTMLElement>();
  let displayQuery = $state("");
  let hasAnimated = $state(false);

  async function animateText(text: string): Promise<void> {
    if (!resultsTextEl) return;

    displayQuery = text;
    await tick();

    const { chars } = splitText(resultsTextEl, {
      chars: { wrap: "clip" },
    });

    animate(chars, {
      y: [{ to: ["100%", "0%"] }],
      duration: 750,
      ease: "out(3)",
      delay: stagger(50),
    });

    hasAnimated = true;
  }

  $effect(() => {
    const q = query.trim();
    const timeout = setTimeout(async () => {
      if (!q) {
        results = [];
        keywords = [];
        qualityMap = new SvelteMap();
        return;
      }
      await animateText(query);
      loading = true;
      const [searchResults, kwResults] = await Promise.all([
        api.search(q),
        api.getKeywords(q),
      ]);
      results = searchResults;
      keywords = kwResults ?? [];
      loading = false;

      if (searchResults.length > 0) {
        const ids = searchResults.map((m) => m.id).join(",");
        fetch(`http://localhost:6969/api/quality/batch?ids=${ids}`)
          .then(async (r) => {
            const reader = r.body!.getReader();
            const decoder = new TextDecoder();
            let buffer = "";

            while (true) {
              const { done, value } = await reader.read();
              if (done) break;
              buffer += decoder.decode(value, { stream: true });
              const lines = buffer.split("\n");
              buffer = lines.pop() ?? "";
              for (const line of lines) {
                if (!line.trim()) continue;
                try {
                  const { id, quality } = JSON.parse(line);
                  qualityMap.set(Number(id), quality);
                } catch {
                  /* empty */
                }
              }
            }
          })
          .catch(() => {});
      }
    }, 400);
    return () => clearTimeout(timeout);
  });
</script>

<div class="relative h-full p-6 pt-18">
  {#if query.length > 0}
    <div
      class="absolute top-18 right-6 left-6 z-10 p-4 shadow-lg"
      style="
      background: linear-gradient(to bottom, var(--background) 0%, var(--background) 70%, rgba(0,0,0,0) 100%);
      pointer-events: none;
    "
    >
      <div class="pointer-events-auto">
        <div
          class="mb-2 flex text-center text-2xl font-semibold"
          class:invisible={!hasAnimated}
        >
          Results for
          <span class="size-1.5"></span>
          {#key displayQuery}
            <span class="text-accent" bind:this={resultsTextEl}
              >{displayQuery}</span
            >
          {/key}
        </div>

        {#if !loading && keywords.length > 1}
          <div class="flex flex-col gap-2 align-middle">
            <span
              class="flex shrink-0 text-center text-xs font-medium text-muted-foreground"
            >
              More to Explore:
            </span>
            <ScrollArea
              orientation="horizontal"
              class="flex-1 overflow-clip rounded-sm"
            >
              <div class="flex gap-2 pb-3">
                {#each keywords as kw (kw.id)}
                  <Button
                    variant="ghost"
                    size="xs"
                    class="text-muted-foreground"
                    onclick={() => {
                      onSuggested(kw.name);
                    }}
                  >
                    {kw.name}
                  </Button>
                {/each}
              </div>
            </ScrollArea>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  {#if !loading}
    <ScrollArea class="h-full">
      <div
        class="mt-32 grid gap-4 pr-4"
        style="grid-template-columns: repeat(auto-fill, minmax(150px, 1fr))"
      >
        {#each results as media (media.id)}
          <MediaCard
            {media}
            onclick={() => {
              onSelectMedia(media);
            }}
            quality={qualityMap.get(media.id) ?? null}
            onsimilar={(m) => {
              onSelectMedia(m);
            }}
            onwatch={onWatch}
          />
        {/each}
      </div>
    </ScrollArea>
  {/if}
</div>
