<script lang="ts">
  import type { Media } from "$lib/types/tmdb";
  import type { Person, PersonDetails } from "$lib/api";
  import { api } from "$lib/api";
  import { animate } from "animejs";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";
  import { Separator } from "$lib/components/ui/separator/index.js";
  import { Cake, MapPin, User, X } from "lucide-svelte";

  let {
    person,
    onclose,
    onselect,
  }: {
    person: Person;
    onclose: () => void;
    onselect: (m: Media) => void;
  } = $props();

  let el = $state<HTMLElement | null>(null);
  let details = $state<PersonDetails | null>(null);
  let loading = $state(true);

  // Show the search-time known_for immediately; swap in the full filmography
  // once the detail fetch lands.
  const credits = $derived(
    details?.credits?.length ? details.credits : (person.known_for ?? []),
  );

  const bioParagraphs = $derived(
    (details?.biography ?? "")
      .split("\n")
      .map((s) => s.trim())
      .filter((s) => s.length > 0),
  );

  $effect(() => {
    loading = true;
    api
      .getPerson(person.id)
      .then((d) => {
        details = d;
      })
      .catch((e) => console.error("PersonExpandedModal details failed", e))
      .finally(() => {
        loading = false;
      });
  });

  function fmtDate(s?: string): string {
    if (!s) return "";
    const d = new Date(s + "T00:00:00");
    if (Number.isNaN(d.getTime())) return s;
    return d.toLocaleDateString(undefined, {
      year: "numeric",
      month: "long",
      day: "numeric",
    });
  }

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

        <!-- Hero: blurred portrait backdrop + sharp portrait + name -->
        <div class="relative isolate w-full overflow-hidden bg-black">
          {#if person.profile_path}
            <img
              src={person.profile_path}
              alt=""
              aria-hidden="true"
              class="absolute inset-0 h-full w-full scale-110 object-cover opacity-40 blur-2xl"
            />
          {/if}

          <div class="relative flex items-end gap-5 p-5 sm:p-7">
            <div
              class="size-28 shrink-0 overflow-hidden rounded-xl border border-white/10 bg-secondary sm:size-36"
            >
              {#if person.profile_path}
                <img
                  src={person.profile_path}
                  alt={person.name}
                  class="h-full w-full object-cover"
                />
              {:else}
                <span class="flex h-full w-full items-center justify-center">
                  <User class="size-10 text-muted-foreground/50" />
                </span>
              {/if}
            </div>

            <div class="min-w-0 pb-1">
              <h2
                class="text-2xl font-bold text-white drop-shadow-lg sm:text-3xl"
              >
                {person.name}
              </h2>
              {#if person.known_for_department}
                <p class="text-sm text-white/70">
                  {person.known_for_department}
                </p>
              {/if}
              <div
                class="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-white/70"
              >
                {#if details?.birthday}
                  <span class="inline-flex items-center gap-1">
                    <Cake class="size-3.5" />
                    {fmtDate(details.birthday)}
                  </span>
                {/if}
                {#if details?.place_of_birth}
                  <span class="inline-flex items-center gap-1">
                    <MapPin class="size-3.5" />
                    {details.place_of_birth}
                  </span>
                {/if}
              </div>
            </div>
          </div>

          <div
            class="pointer-events-none absolute inset-x-0 bottom-0 h-1/3"
            style="background: linear-gradient(to top, var(--background) 2%, transparent 100%)"
          ></div>
        </div>

        <!-- Body -->
        <div class="flex flex-col gap-5 p-5 sm:p-7">
          {#if bioParagraphs.length}
            <div class="flex flex-col gap-3">
              {#each bioParagraphs.slice(0, 4) as para, i (i)}
                <p class="text-sm leading-relaxed text-muted-foreground">
                  {para}
                </p>
              {/each}
            </div>
          {:else if loading}
            <p class="animate-pulse text-sm text-muted-foreground">Loading…</p>
          {/if}

          {#if credits.length}
            <div class="space-y-3">
              <Separator />
              <h3 class="text-base font-semibold">Known for</h3>
              <div class="grid grid-cols-3 gap-3 sm:grid-cols-4 lg:grid-cols-6">
                {#each credits as item (item.media_type + "-" + item.id)}
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
            </div>
          {/if}
        </div>
      </div>
    </div>
  </ScrollArea>
</div>
