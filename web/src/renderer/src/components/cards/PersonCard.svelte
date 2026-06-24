<script lang="ts">
  import type { Person } from "$lib/api";
  import { User } from "lucide-svelte";

  let { person, onclick } = $props<{
    person: Person;
    onclick?: (p: Person) => void;
  }>();
</script>

<button
  onclick={() => onclick?.(person)}
  class="group flex w-full flex-col gap-2 text-left"
  aria-label={person.name}
>
  <span
    class="relative block h-24 w-24 overflow-hidden rounded-full bg-secondary"
  >
    {#if person.profile_path}
      <img
        src={person.profile_path}
        alt={person.name}
        class="h-full w-full object-cover transition-transform duration-200 group-hover:scale-105"
      />
    {:else}
      <span class="flex h-full w-full items-center justify-center">
        <User class="size-8 text-muted-foreground/40" />
      </span>
    {/if}
  </span>

  <span class="min-w-0">
    <span class="block truncate text-sm font-semibold">{person.name}</span>
    {#if person.known_for_department}
      <span class="block truncate text-xs text-muted-foreground">
        {person.known_for_department}
      </span>
    {/if}
  </span>
</button>
