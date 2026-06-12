<script lang="ts">
  import { Minus, Square, X, Search } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button";
  import * as ButtonGroup from "$lib/components/ui/button-group/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import { Spinner } from "$lib/components/ui/spinner/index.js";
  import DarkModeButton from "./DarkModeButton.svelte";

  function minimize(): void {
    window.electron.ipcRenderer.send("window-minimize");
  }

  function maximize(): void {
    window.electron.ipcRenderer.send("window-maximize");
  }

  function close(): void {
    window.electron.ipcRenderer.send("window-close");
  }

  let { query = $bindable(""), loading = $bindable(false) } = $props();
</script>

<div
  class="fixed z-50 flex h-12 w-full items-center justify-between px-6 pt-6 select-none [webkit-app-region:drag]"
>
  <div class="flex items-center gap-2">
    <span class="text-2xl font-bold tracking-wider text-orange-400">COVE</span>
  </div>

  <!-- Ensure this container is non-draggable and interactive -->
  <div class="flex w-full items-center gap-2 p-5 [webkit-app-region:no-drag]">
    <div class="relative w-full">
      {#if loading}
        <Spinner
          class="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
        />
      {:else}
        <Search
          class="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
        />
      {/if}
      <Input
        type="search"
        placeholder="Search..."
        class="h-8 pl-8"
        bind:value={query}
      />
    </div>
  </div>

  <div class="flex items-center gap-1 [webkit-app-region:no-drag]">
    <DarkModeButton />
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
