<script lang="ts">
  import TopBar from "./components/TopBar.svelte";
  import { ModeWatcher } from "mode-watcher";
  import MediaCard from "./components/MediaCard.svelte";
  import type { Media } from "$lib/types/tmdb";
  import MediaPage from "./components/MediaPage.svelte";
  import * as Tooltip from "$lib/components/ui/tooltip";

  import type { Page } from "$lib/types/types";
  import QueryPage from "./components/QueryPage.svelte";
  import HomePage from "./components/HomePage.svelte";

  let query = $state("");

  let selectedMedia: Media | null = $state(null);
  let selectedSimilar = $state<Media | null>(null);

  let loading = $state(false);

  let currentPage = $state<Page>({ type: "home" });
  let pageHistory = $state<Page[]>([]);

  function changePage(page: Page): void {
    pageHistory.push(currentPage);
    currentPage = page;

    if (pageHistory.length > 25) {
      pageHistory.shift();
    }
  }

  function goBack(): void {
    const previousPage = pageHistory.pop();
    if (previousPage) {
      currentPage = previousPage;
      if (previousPage.type === "mediaView") {
        selectedMedia = previousPage.media;
      }
      if (previousPage.type === "query") {
        query = previousPage.query;
      }
    }
  }

  async function selectMedia(media: Media): Promise<void> {
    selectedMedia = media;
    changePage({ type: "mediaView", media });
  }
</script>

<Tooltip.Provider>
  <TopBar
    bind:query
    bind:loading
    onSelectPage={changePage}
    bind:pageHistory
    onGoBack={goBack}
  />
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
        />
      {:else if currentPage.type === "query"}
        <QueryPage
          bind:query
          bind:loading
          onSelectMedia={selectMedia}
          onSuggested={(name: string) => {
            query = name;
            changePage({ type: "query", query: name });
          }}
        />
      {:else if currentPage.type === "home"}
        <HomePage />
      {/if}
    </main>
  </div>
</Tooltip.Provider>
<ModeWatcher />
