<script lang="ts">
  import { Minus, Square, X, Search, House, CirclePlus } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button";
  import * as ButtonGroup from "$lib/components/ui/button-group/index.js";
  import { Spinner } from "$lib/components/ui/spinner/index.js";
  import CoveIcon from "../assets/CoveIcon.svelte";
  import { animate } from "animejs";

  function minimize(): void {
    window.electron.ipcRenderer.send("window-minimize");
  }

  function maximize(): void {
    window.electron.ipcRenderer.send("window-maximize");
  }

  function close(): void {
    window.electron.ipcRenderer.send("window-close");
  }

  let {
    query = $bindable(""),
    loading = $bindable(false),
    onSelectPage,
  } = $props();

  let searchOuter = $state<HTMLDivElement>();
  let searchState = $state<"active" | "hidden">("hidden");
  let searchFocused = $state<boolean>(false);

  async function toggleSearch(show: boolean): Promise<void> {
    if (show === (searchState === "active")) return;
    if (query.length > 0 && searchFocused) return;

    animate(searchOuter, {
      width: show ? 300 : 36,
      duration: 300,
      easing: "easeOutExpo",
      complete: () => {
        searchState = show ? "active" : "hidden";
      },
    });
  }

  function selectPage(page: string): void {
    onSelectPage({ type: page });
  }
</script>

<div
  class="fixed z-50 flex h-12 w-full items-center justify-between px-6 pt-6 select-none [webkit-app-region:drag]"
>
  <div class="flex items-center gap-2">
    <span class="text-2xl font-bold tracking-wider text-orange-400">
      <CoveIcon />
    </span>
  </div>

  <div class="flex items-center gap-2 p-5 [webkit-app-region:no-drag]">
    <div class="flex items-center gap-1">
      <ButtonGroup.Root>
        <Button
          variant="outline"
          size="default"
          onclick={() => {
            selectPage("home");
          }}
        >
          <House />
        </Button>
        <Button
          variant="outline"
          size="default"
          onclick={() => {
            selectPage("myList");
          }}
        >
          <CirclePlus />
        </Button>
      </ButtonGroup.Root>
    </div>
    <div
      bind:this={searchOuter}
      class="relative flex h-9 items-center rounded-full border bg-transparent"
      class:w-9={searchState === "hidden"}
      class:w-[300px]={searchState === "active"}
      role="search"
      onmouseenter={() => toggleSearch(true)}
      onmouseleave={() => toggleSearch(false)}
    >
      <div
        class="pointer-events-none absolute top-1/2 transition-all duration-300"
        class:left-2.5={searchState === "active"}
        style:left={searchState === "hidden" ? "50%" : undefined}
        style:transform={searchState === "hidden"
          ? "translate(-50%, -50%)"
          : "translateY(-50%)"}
      >
        {#if loading}
          <Spinner class="size-4" />
        {:else}
          <Search class="size-4" />
        {/if}
      </div>

      <input
        type="search"
        placeholder="Search..."
        class="h-full w-full border-0 bg-transparent pr-2 pl-8 text-sm outline-none focus:ring-0"
        class:opacity-0={searchState === "hidden"}
        class:opacity-100={searchState === "active"}
        bind:value={query}
        disabled={searchState === "hidden"}
        onfocus={() => {
          searchFocused = true;
        }}
        onfocusout={() => {
          searchFocused = false;
          toggleSearch(false);
        }}
      />
    </div>
  </div>

  <div class="flex items-center gap-1 [webkit-app-region:no-drag]">
    <ButtonGroup.Root>
      <Button variant="outline" size="icon-sm" onclick={minimize}>
        <Minus />
      </Button>
      <Button variant="outline" size="icon-sm" onclick={maximize}>
        <Square />
      </Button>
      <Button variant="outline" size="icon-sm" onclick={close}>
        <X />
      </Button>
    </ButtonGroup.Root>
  </div>
</div>
