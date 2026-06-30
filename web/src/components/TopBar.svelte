<script lang="ts">
  import {
    X,
    Search,
    House,
    ArrowLeft,
    Flame,
    Cog,
    Bookmark,
    Maximize2,
    Minimize2,
  } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button";
  import { Spinner } from "$lib/components/ui/spinner/index.js";
  import CoveIcon from "../assets/CoveIcon.svelte";
  import { animate, JSAnimation } from "animejs";
  import type { Page } from "$lib/types/types";
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import { Player } from "$lib/player/player.svelte";
  import AccountPopover from "./AccountPopover.svelte";

  let {
    query = $bindable(""),
    loading = $bindable(false),
    onSelectPage = () => {},
    canGoBack = false,
    onGoBack,
    fullscreenInfo = null,
    onCloseStream,
    currentPage,
  }: {
    query?: string;
    loading?: boolean;
    onSelectPage?: (p: Page) => void;
    canGoBack?: boolean;
    onGoBack?: () => void;
    fullscreenInfo?: { title: string; subtitle?: string } | null;
    onCloseStream?: () => void;
    currentPage?: Page;
  } = $props();

  let searchOuter = $state<HTMLDivElement>();
  let searchState = $state<"active" | "hidden">("hidden");
  let searchFocused = $state<boolean>(false);
  let topbarHovered = $state<boolean>(false);
  let debounceTimer: ReturnType<typeof setTimeout>;

  // Auto-hide when a stream is playing; reappear on any mouse movement.
  let topbarVisible = $state(true);
  let hideTimer: ReturnType<typeof setTimeout> | undefined;

  function revealTopbar(): void {
    topbarVisible = true;
    clearTimeout(hideTimer);
    if (fullscreenInfo) {
      hideTimer = setTimeout(() => { topbarVisible = false; }, 3000);
    }
  }

  $effect(() => {
    if (fullscreenInfo) {
      revealTopbar();
      document.addEventListener("mousemove", revealTopbar);
      return () => {
        document.removeEventListener("mousemove", revealTopbar);
        clearTimeout(hideTimer);
        topbarVisible = true;
      };
    } else {
      clearTimeout(hideTimer);
      topbarVisible = true;
    }
  });

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
    // While the input is focused, leave it open no matter what's in it —
    // clicking into an empty box and then moving the mouse off the bar
    // shouldn't collapse it out from under you mid-interaction.
    if (searchFocused) return;

    animate(searchOuter, {
      width: show ? 300 : 36,
      duration: 300,
      easing: "easeOutExpo",
      complete: () => {
        searchState = show ? "active" : "hidden";
      },
    });
  }

  function selectPage(
    page: Exclude<Page, { type: "mediaView" }>["type"],
  ): void {
    onSelectPage({ type: page } as Page);
  }

  function openQuery(): void {
    clearTimeout(debounceTimer);

    debounceTimer = setTimeout(() => {
      onSelectPage({ type: "query", query });
    }, 500);
  }
</script>

<div
  class="fixed z-50 flex h-12 w-full items-center justify-between px-6 pt-6 select-none [webkit-app-region:drag] transition-opacity duration-300 {fullscreenInfo && !topbarVisible ? 'opacity-0 pointer-events-none' : 'opacity-100'}"
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
  <div class="flex w-48 min-w-0 items-center gap-2">
    {#if fullscreenInfo}
      <div class="flex min-w-0 flex-col leading-tight">
        <span class="max-w-64 truncate text-sm font-semibold text-white">
          {fullscreenInfo.title}
        </span>
        {#if fullscreenInfo.subtitle}
          <span class="max-w-64 truncate text-xs text-white/60">
            {fullscreenInfo.subtitle}
          </span>
        {/if}
      </div>
    {:else}
      <span class="text-2xl font-bold tracking-wider text-accent">
        <CoveIcon size={32} />
      </span>
    {/if}
  </div>

  {#if !fullscreenInfo}
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
                      disabled={!canGoBack}
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
                      class="rounded-none border-l-0 transition-all {currentPage?.type ===
              'home'
                ? 'text-accent hover:text-accent/75'
                : 'bg-foreground'}"
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
                      class="rounded-none border-l-0 {currentPage?.type === 'myList'
                ? 'text-accent hover:text-accent/75'
                : 'bg-foreground'}"
                      onclick={() => {
                selectPage("myList");
              }}
              >
                <Bookmark />
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
                      class="rounded-none border-l-0 {currentPage?.type === 'explore'
                ? 'text-accent hover:text-accent/75'
                : 'bg-foreground'}"
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
  {/if}

  <div
    class="flex w-48 items-center justify-end gap-1 [webkit-app-region:no-drag]"
  >
    {#if fullscreenInfo}
      <Tooltip.Root>
        <Tooltip.Trigger>
          <Button variant="outline" size="icon" onclick={() => Player.toggleFullscreen()}>
            {#if Player.isFullscreen}
              <Minimize2 />
            {:else}
              <Maximize2 />
            {/if}
          </Button>
        </Tooltip.Trigger>
        <Tooltip.Content>
          <p>{Player.isFullscreen ? "Exit fullscreen" : "Fullscreen"}</p>
        </Tooltip.Content>
      </Tooltip.Root>
      <Tooltip.Root>
        <Tooltip.Trigger>
          <Button variant="outline" size="icon" onclick={onCloseStream}>
            <X />
          </Button>
        </Tooltip.Trigger>
        <Tooltip.Content>
          <p>Close</p>
        </Tooltip.Content>
      </Tooltip.Root>
    {:else}
      <Tooltip.Root>
        <Tooltip.Trigger>
          <Button
            variant="outline"
            size="icon"
            onclick={() => {
              selectPage("settings");
            }}
          >
            <Cog />
          </Button>
        </Tooltip.Trigger>
        <Tooltip.Content>
          <p>Settings</p>
        </Tooltip.Content>
      </Tooltip.Root>
      <AccountPopover />
    {/if}
  </div>
</div>
