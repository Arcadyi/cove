<script lang="ts">
  import { CircleUser, Check, Plus, RefreshCw, LogOut, LogIn } from "lucide-svelte";
  import * as Popover from "$lib/components/ui/popover/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Separator } from "$lib/components/ui/separator/index.js";
  import { Spinner } from "$lib/components/ui/spinner/index.js";
  import { auth } from "$lib/stores/auth.svelte";
  import { api } from "$lib/api";
  import AuthDialog from "./AuthDialog.svelte";
  import { libraryChanged } from "$lib/stores/library";

  let open = $state(false);
  let syncing = $state(false);
  let authOpen = $state(false);
  let creatingProfile = $state(false);
  let newProfileName = $state("");
  let showNewProfileInput = $state(false);

  const initials = $derived(
    auth.activeProfile?.name
      ? auth.activeProfile.name.charAt(0).toUpperCase()
      : "?",
  );

  async function switchProfile(id: string): Promise<void> {
    try {
      const profile = await api.profileActivate(id);
      const profs = await api.profilesList();
      auth.setProfiles(
        profs.profiles,
        profs.profiles.find((p) => p.id === profile.id) ?? profile,
      );
      libraryChanged.update((n) => n + 1);
      open = false;
    } catch (e) {
      console.error("switch profile:", e);
    }
  }

  async function createProfile(): Promise<void> {
    const name = newProfileName.trim();
    if (!name) return;
    creatingProfile = true;
    try {
      await api.profileCreate(name);
      const profs = await api.profilesList();
      auth.setProfiles(
        profs.profiles,
        profs.profiles.find((p) => p.id === profs.active_profile_id) ??
          profs.profiles[0],
      );
      newProfileName = "";
      showNewProfileInput = false;
    } catch (e) {
      console.error("create profile:", e);
    } finally {
      creatingProfile = false;
    }
  }

  async function sync(): Promise<void> {
    syncing = true;
    try {
      await api.authSync();
      libraryChanged.update((n) => n + 1);
    } catch (e) {
      console.error("sync:", e);
    } finally {
      syncing = false;
    }
  }

  async function logout(): Promise<void> {
    await api.authLogout();
    await auth.logout();
    open = false;
  }
</script>

<Popover.Root bind:open>
  <Popover.Trigger>
    {#snippet child({ props })}
      <Button variant="outline" size="icon" {...props}>
        {#if auth.session}
          <span class="flex size-full items-center justify-center rounded-full bg-accent text-xs font-semibold text-accent-foreground">
            {initials}
          </span>
        {:else}
          <CircleUser class="size-4 text-muted-foreground" />
        {/if}
      </Button>
    {/snippet}
  </Popover.Trigger>

  <Popover.Content class="w-64 p-0" align="end" sideOffset={8}>
    <div class="flex flex-col">
      <!-- Profile list -->
      <div class="px-3 py-2">
        <p class="pb-1 text-xs font-medium text-muted-foreground">Profiles</p>
        {#each auth.profiles as profile (profile.id)}
          <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
          <div
            class="flex cursor-pointer items-center justify-between rounded-md px-2 py-1.5 text-sm hover:bg-accent hover:text-accent-foreground"
            onclick={() => switchProfile(profile.id)}
          >
            <span class="flex items-center gap-2">
              <span class="flex size-6 shrink-0 items-center justify-center rounded-full bg-secondary text-xs font-semibold">
                {profile.name.charAt(0).toUpperCase()}
              </span>
              <span class="truncate">{profile.name}</span>
            </span>
            {#if auth.activeProfile?.id === profile.id}
              <Check class="size-3.5 shrink-0 text-accent" />
            {/if}
          </div>
        {/each}

        <!-- New profile input -->
        {#if showNewProfileInput}
          <div class="mt-1 flex items-center gap-1">
            <input
              type="text"
              placeholder="Profile name"
              class="min-w-0 flex-1 rounded-md border border-input bg-background px-2 py-1 text-xs outline-none focus:ring-1 focus:ring-ring"
              bind:value={newProfileName}
              onkeydown={(e) => { if (e.key === "Enter") createProfile(); if (e.key === "Escape") { showNewProfileInput = false; newProfileName = ""; } }}
              autofocus
            />
            <Button size="icon" class="size-6 shrink-0" onclick={createProfile} disabled={creatingProfile}>
              {#if creatingProfile}<Spinner class="size-3" />{:else}<Plus class="size-3" />{/if}
            </Button>
          </div>
        {:else}
          <button
            class="mt-1 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            onclick={() => (showNewProfileInput = true)}
          >
            <Plus class="size-3.5" /> New profile
          </button>
        {/if}
      </div>

      <Separator />

      <!-- Account section -->
      <div class="px-3 py-2">
        <p class="pb-1 text-xs font-medium text-muted-foreground">Account</p>
        {#if auth.session}
          <p class="mb-2 truncate text-xs text-muted-foreground">{auth.session.email}</p>
          <div class="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              class="flex-1 gap-1 text-xs"
              onclick={sync}
              disabled={syncing}
            >
              {#if syncing}<Spinner class="size-3" />{:else}<RefreshCw class="size-3" />{/if}
              Sync now
            </Button>
            <Button
              variant="ghost"
              size="sm"
              class="gap-1 text-xs text-muted-foreground"
              onclick={logout}
            >
              <LogOut class="size-3" /> Sign out
            </Button>
          </div>
        {:else}
          <p class="mb-2 text-xs text-muted-foreground">Guest — data is local only</p>
          <Button
            variant="default"
            size="sm"
            class="w-full gap-1 text-xs"
            onclick={() => { open = false; authOpen = true; }}
          >
            <LogIn class="size-3" /> Sign in / Create account
          </Button>
        {/if}
      </div>
    </div>
  </Popover.Content>
</Popover.Root>

{#if authOpen}
  <AuthDialog onclose={() => (authOpen = false)} />
{/if}
