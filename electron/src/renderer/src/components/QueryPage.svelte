<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";
  import MediaCard from "./MediaCard.svelte";
  import { SvelteMap } from "svelte/reactivity";
  import { api } from "$lib/api";

  let {
    query = $bindable(""),
    loading = $bindable(true),
    onSelectMedia,
  } = $props();

  let results: Media[] = $state([]);
  let keywords: { id: number; name: string }[] = $state([]);
  // svelte-ignore non_reactive_update
  let qualityMap = new SvelteMap<number, string>();

  $effect(() => {
    const q = query.trim();
    const timeout = setTimeout(async () => {
      if (!q) {
        results = [];
        keywords = [];
        qualityMap = new SvelteMap();
        return;
      }
      loading = true;
      const [searchResults, kwResults] = await Promise.all([
        api.search(q),
        fetch(`http://localhost:6969/api/keywords?q=${encodeURIComponent(q)}`)
          .then((r) => r.json())
          .catch(() => []),
      ]);
      results = searchResults;
      keywords = kwResults ?? [];
      loading = false;

      // Batch fetch quality for all results
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

<div class="h-full p-6 pt-18">
  {#if keywords.length > 1}
    <div class="mb-4 flex flex-col items-start gap-2">
      <span class="shrink-0 text-xs font-medium text-muted-foreground">
        More to explore:
      </span>
      <ScrollArea
        orientation="horizontal"
        class="flex-1 overflow-clip rounded-sm"
      >
        <div class="flex gap-2 pb-3">
          {#each keywords as kw (kw.id)}
            <button
              class="shrink-0 rounded-full border border-border bg-secondary px-3 py-1 text-xs text-secondary-foreground transition-colors hover:bg-primary hover:text-primary-foreground"
              onclick={() => (query = kw.name)}
            >
              {kw.name}
            </button>
          {/each}
        </div>
      </ScrollArea>
    </div>
  {/if}
  <ScrollArea class="h-full">
    <div
      class="mt-4 grid gap-4 pr-4"
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
        />
      {/each}
    </div>
  </ScrollArea>
</div>
