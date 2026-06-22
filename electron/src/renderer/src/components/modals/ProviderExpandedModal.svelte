<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import type { Provider } from "$lib/api";
  import { api } from "$lib/api";
  import { animate } from "animejs";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";
  import { MonitorPlay, X } from "lucide-svelte";

  let {
    provider,
    onclose,
    onselect,
  }: {
    provider: Provider;
    onclose: () => void;
    onselect: (m: Media) => void;
  } = $props();

  let el = $state<HTMLElement | null>(null);
  let titles = $state<Media[]>([]);
  let loading = $state(true);

  $effect(() => {
    loading = true;
    api
      .providerTitles(provider.provider_id, 40)
      .then((t) => {
        titles = t ?? [];
      })
      .catch((e) => console.error("ProviderExpandedModal titles failed", e))
      .finally(() => {
        loading = false;
      });
  });

  function titleOf(m: Media): string {
    return m.media_type === "tv" ? m.name : m.title;
  }

  $effect(() => {
    if (!el) return;
    animate(el, {
      scale: [0.94, 1],
      opacity: [0, 1],
      duration: 220,
      easing: "easeOutQuart",
    });
  });

  function close(): void {
    if (!el) {
      onclose();
      return;
    }
    animate(el, {
      scale: [1, 0.94],
      opacity: [1, 0],
      duration: 180,
      easing: "easeInQuart",
      onComplete: onclose,
    });
  }
</script>

<div
  class="pointer-events-none fixed inset-0 z-40 bg-black/70 backdrop-blur-sm"
></div>

<div class="fixed inset-0 z-50 mt-18">
  <ScrollArea class="h-full w-full">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      role="presentation"
      class="flex min-h-full items-start justify-center overscroll-contain p-4 sm:p-6 lg:p-10"
      onmousedown={close}
    >
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        bind:this={el}
        role="presentation"
        class="relative my-auto flex w-[min(1080px,94vw)] cursor-default flex-col overflow-hidden rounded-xl border border-border bg-background shadow-2xl"
        onmousedown={(e) => e.stopPropagation()}
        onclick={(e) => e.stopPropagation()}
        onkeydown={(e) => e.stopPropagation()}
        style="opacity: 0; transform: scale(0.94);"
      >
        <button
          class="absolute top-3 right-3 z-20 flex size-9 items-center justify-center rounded-full bg-black/60 text-white transition hover:bg-black/80"
          onclick={close}
          aria-label="Close"
        >
          <X class="size-5" />
        </button>

        <!-- Hero: blurred logo backdrop + logo tile + name -->
        <div class="relative isolate w-full overflow-hidden bg-black">
          {#if provider.logo_path}
            <img
              src={provider.logo_path}
              alt=""
              aria-hidden="true"
              class="absolute inset-0 h-full w-full scale-125 object-cover opacity-30 blur-3xl"
            />
          {/if}

          <div class="relative flex items-center gap-5 p-5 sm:p-7">
            <div
              class="size-20 shrink-0 overflow-hidden rounded-2xl border bg-card sm:size-24"
            >
              {#if provider.logo_path}
                <img
                  src={provider.logo_path}
                  alt={provider.provider_name}
                  class="h-full w-full object-contain p-2"
                />
              {:else}
                <span class="flex h-full w-full items-center justify-center">
                  <MonitorPlay class="size-8 text-muted-foreground/50" />
                </span>
              {/if}
            </div>

            <div class="min-w-0">
              <p class="text-xs text-white/60">Streaming service</p>
              <h2
                class="text-2xl font-bold text-white drop-shadow-lg sm:text-3xl"
              >
                {provider.provider_name}
              </h2>
            </div>
          </div>

          <div
            class="pointer-events-none absolute inset-x-0 bottom-0 h-1/2"
            style="background: linear-gradient(to top, var(--background) 2%, transparent 100%)"
          ></div>
        </div>

        <!-- Body -->
        <div class="flex flex-col gap-4 p-5 sm:p-7">
          <h3 class="text-base font-semibold">
            Popular on {provider.provider_name}
          </h3>

          {#if loading}
            <div class="grid grid-cols-3 gap-3 sm:grid-cols-4 lg:grid-cols-6">
              {#each { length: 12 } as _, i (i)}
                <div
                  class="aspect-2/3 w-full animate-pulse rounded-md bg-muted"
                ></div>
              {/each}
            </div>
          {:else if titles.length}
            <div class="grid grid-cols-3 gap-3 sm:grid-cols-4 lg:grid-cols-6">
              {#each titles as item (item.media_type + "-" + item.id)}
                <div
                  role="button"
                  tabindex="0"
                  class="cursor-pointer overflow-hidden rounded-md"
                  onclick={() => onselect(item)}
                  onkeydown={(e) => e.key === "Enter" && onselect(item)}
                >
                  <img
                    src={item.poster_path}
                    alt={titleOf(item)}
                    class="aspect-2/3 w-full object-cover transition-opacity hover:opacity-75"
                  />
                </div>
              {/each}
            </div>
          {:else}
            <p class="text-sm text-muted-foreground">
              No titles found for this provider in your region (US).
            </p>
          {/if}
        </div>
      </div>
    </div>
  </ScrollArea>
</div>
