<script lang="ts">
  import { api } from "$lib/api";
  import TopBar from "./components/TopBar.svelte";
  import { ModeWatcher } from "mode-watcher";
  import MediaCard from "./components/MediaCard.svelte";
  import type { Media } from "$lib/types/tmdb";
  import MediaPage from "./components/MediaPage.svelte";
  import * as Tooltip from "$lib/components/ui/tooltip";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";
  import { SvelteMap } from "svelte/reactivity";
  import type { Page } from "$lib/types/types";

  let query = $state("");
  let results: Media[] = $state([]);
  let selectedMedia: Media | null = $state(null);
  let selectedSimilar = $state<Media | null>(null);
  // svelte-ignore non_reactive_update
  let qualityMap = new SvelteMap<number, string>();
  let loading = $state(false);
  let keywords: { id: number; name: string }[] = $state([]);

  let currentPage = $state<Page>({ type: "home" });
  let pageHistory = $state<Page[]>([]);

  function changePage(page: Page): void {
    pageHistory.push(currentPage);
    currentPage = page;
  }

  function goBack(): void {
    const previousPage = pageHistory.pop();
    if (previousPage) {
      currentPage = previousPage;
      if (previousPage.type === "mediaView") {
        selectedMedia = previousPage.media;
      }
    }
  }

  $effect(() => {
    selectedMedia = null;
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

  async function selectMedia(media: Media): Promise<void> {
    selectedMedia = media;
    changePage({ type: "mediaView", media });
  }
</script>

<Tooltip.Provider>
  <TopBar bind:query bind:loading onSelectPage={changePage} />
  {#if selectedSimilar}
    {#key selectedSimilar.id}
      <MediaCard
        media={selectedSimilar}
        onclick={() => {}}
        initialExpanded={true}
        onclose={() => (selectedSimilar = null)}
        onsimilar={(m) => (selectedSimilar = m)}
      />
    {/key}
  {/if}
  <div class="flex h-screen flex-col overflow-hidden">
    <main class="relative min-h-0 flex-1 overflow-hidden">
      {#if selectedMedia && currentPage.type === "mediaView"}
        <MediaPage
          media={selectedMedia}
          onsimilar={(m) => {
            selectMedia(m);
          }}
          onBack={() => goBack()}
        />
      {:else}
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
                    selectMedia(media);
                  }}
                  quality={qualityMap.get(media.id) ?? null}
                  onsimilar={(m) => (selectedSimilar = m)}
                />
              {/each}
            </div>
          </ScrollArea>
        </div>
      {/if}
    </main>
  </div>
</Tooltip.Provider>
<ModeWatcher />
