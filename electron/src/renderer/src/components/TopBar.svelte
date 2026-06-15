<script lang="ts">
  import {
    Minus,
    Square,
    X,
    Search,
    House,
    CirclePlus,
    ArrowLeft,
    Flame,
  } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button";
  import * as ButtonGroup from "$lib/components/ui/button-group/index.js";
  import { Spinner } from "$lib/components/ui/spinner/index.js";
  import CoveIcon from "../assets/CoveIcon.svelte";
  import {animate, JSAnimation} from "animejs";
  import type { Page } from "$lib/types/types";
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";

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
    onSelectPage = (p: Page) => {},
    pageHistory = $bindable([]),
    onGoBack,
  } = $props();

  let searchOuter = $state<HTMLDivElement>();
  let searchState = $state<"active" | "hidden">("hidden");
  let searchFocused = $state<boolean>(false);
  let topbarHovered = $state<boolean>(false);
  let debounceTimer: ReturnType<typeof setTimeout>;

  // Animation states
  let rectEl = $state<SVGRectElement>();
  let traceAnimation = $state<JSAnimation | null>(null);

  $effect(() => {
    if (loading && rectEl) {
      const length = rectEl.getTotalLength();
      // Set the dasharray: first value is the visible line length (25% of total), second is the gap
      rectEl.style.strokeDasharray = `${length * 0.25} ${length}`;

      traceAnimation = animate(rectEl, {
        strokeDashoffset: [length, 0],
        duration: 1000,
        easing: "inBounce",
        loop: true,
      });
    } else if (traceAnimation) {
      traceAnimation.pause();
      traceAnimation = null;
    }
  });

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

  function openQuery(): void {
    clearTimeout(debounceTimer);

    debounceTimer = setTimeout(() => {
      onSelectPage({ type: "query", query });
    }, 500);
  }
</script>

<div
  class="fixed z-50 flex h-12 w-full items-center justify-between px-6 pt-6 select-none [webkit-app-region:drag]"
  role="menubar"
  tabindex="0"
  onmouseenter={() => {
    topbarHovered = true;
  }}
  onmouseleave={() => {
    topbarHovered = false;
    toggleSearch(false);
  }}
>
  <div class="flex items-center gap-2">
    <span class="text-2xl font-bold tracking-wider text-accent">
      <CoveIcon />
    </span>
  </div>

  <div
    class="relative flex items-center rounded-full bg-background [webkit-app-region:no-drag]"
  >
    <svg
      class="pointer-events-none absolute inset-0 z-0 h-full w-full transition-opacity duration-300"
      class:opacity-100={loading}
      class:opacity-0={!loading}
      xmlns="http://www.w3.org/2000/svg"
    >
      <rect
        bind:this={rectEl}
        x="0"
        y="0"
        width="100%"
        height="100%"
        rx="20"
        fill="none"
        class="stroke-accent"
        stroke-width="2"
        stroke-linecap="round"
      />
    </svg>

    <div class="relative z-10 flex items-center">
      <div class="flex items-center">
        <Tooltip.Root>
          <Tooltip.Trigger>
            <Button
              variant="outline"
              size="icon"
              class="rounded-l-full rounded-r-none"
              disabled={pageHistory.length < 1}
              onclick={onGoBack}
            >
              <ArrowLeft />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>
            <div class="flex flex-col">
              <p>Back</p>
            </div>
          </Tooltip.Content>
        </Tooltip.Root>

        <Tooltip.Root>
          <Tooltip.Trigger>
            <Button
              variant="outline"
              size="icon"
              class="rounded-none border-l-0"
              onclick={() => {
                selectPage("home");
              }}
            >
              <House />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>
            <p>Home</p>
          </Tooltip.Content>
        </Tooltip.Root>

        <Tooltip.Root>
          <Tooltip.Trigger>
            <Button
              variant="outline"
              size="icon"
              class="rounded-none border-l-0"
              onclick={() => {
                selectPage("myList");
              }}
            >
              <CirclePlus />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>
            <p>My List</p>
          </Tooltip.Content>
        </Tooltip.Root>

        <Tooltip.Root>
          <Tooltip.Trigger>
            <Button
              variant="outline"
              size="icon"
              class="rounded-none border-l-0"
              onclick={() => {
                selectPage("explore");
              }}
            >
              <Flame />
            </Button>
          </Tooltip.Trigger>
          <Tooltip.Content>
            <p>Explore</p>
          </Tooltip.Content>
        </Tooltip.Root>
      </div>

      <div
        bind:this={searchOuter}
        class="relative flex h-9 items-center rounded-l-none rounded-r-full border border-l-0 bg-transparent"
        class:w-9={searchState === "hidden"}
        class:w-[300px]={searchState === "active"}
        role="search"
        onmouseenter={() => toggleSearch(true)}
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
            if (query.length > 0) {
              openQuery();
            }
          }}
          onfocusout={() => {
            searchFocused = false;
            if (!topbarHovered) {
              toggleSearch(false);
            }
          }}
          oninput={openQuery}
        />
      </div>
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
