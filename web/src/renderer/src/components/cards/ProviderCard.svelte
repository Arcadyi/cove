<script lang="ts">
  import type { Provider } from "$lib/api";
  import { MonitorPlay } from "lucide-svelte";

  let { provider, onclick } = $props<{
    provider: Provider;
    onclick?: (p: Provider) => void;
  }>();
</script>

<button
  onclick={() => onclick?.(provider)}
  class="group flex w-full flex-col items-center gap-2 text-center"
  aria-label={provider.provider_name}
>
  <span
    class="relative block aspect-square w-full overflow-hidden rounded-xl border bg-card"
  >
    {#if provider.logo_path}
      <img
        src={provider.logo_path}
        alt={provider.provider_name}
        class="h-full w-full object-contain p-3 transition-transform duration-200 group-hover:scale-105"
      />
    {:else}
      <span class="flex h-full w-full items-center justify-center">
        <MonitorPlay class="size-8 text-muted-foreground/40" />
      </span>
    {/if}
  </span>

  <span class="block w-full truncate text-xs font-medium">
    {provider.provider_name}
  </span>
</button>
