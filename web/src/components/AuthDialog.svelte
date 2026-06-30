<script lang="ts">
  import { X, Mail, Lock, User, CheckCircle } from "lucide-svelte";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";
  import {
    InputOTP,
    InputOTPGroup,
    InputOTPSlot,
    InputOTPSeparator,
  } from "$lib/components/ui/input-otp/index.js";
  import { api } from "$lib/api";
  import { auth } from "$lib/stores/auth.svelte";
  import { Spinner } from "$lib/components/ui/spinner/index.js";
  import { libraryChanged } from "$lib/stores/library";

  let { onclose }: { onclose: () => void } = $props();

  type View = "choose" | "login" | "otp-email" | "otp-code" | "register" | "register-otp" | "success";
  let view = $state<View>("choose");
  let loading = $state(false);
  let error = $state("");

  // Form fields
  let email = $state("");
  let password = $state("");
  let profileName = $state("");
  let otpCode = $state("");
  let otpEmail = $state("");

  // Pending registration — carried from register view into register-otp view
  let pendingEmail = $state("");
  let pendingPassword = $state("");
  let pendingProfileName = $state("");

  function reset(): void {
    email = "";
    password = "";
    profileName = "";
    otpCode = "";
    otpEmail = "";
    error = "";
    loading = false;
  }

  function setView(v: View): void {
    reset();
    view = v;
  }

  async function login(): Promise<void> {
    if (!email || !password) { error = "Email and password are required."; return; }
    loading = true; error = "";
    try {
      const res = await api.authLogin(email, password);
      auth.setSession(res.access_token, email, res.profiles, res.active, res.refresh_token);
      libraryChanged.update((n) => n + 1);
      onclose();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function sendOTP(): Promise<void> {
    if (!email) { error = "Email is required."; return; }
    loading = true; error = "";
    try {
      await api.authSendOTP(email);
      otpEmail = email;
      view = "otp-code";
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function verifyOTP(): Promise<void> {
    if (loading || !otpCode || otpCode.length < 8) return;
    loading = true; error = "";
    try {
      const res = await api.authVerifyOTP(otpEmail, otpCode);
      auth.setSession(res.access_token, otpEmail, res.profiles, res.active, res.refresh_token);
      libraryChanged.update((n) => n + 1);
      onclose();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      otpCode = "";
    } finally {
      loading = false;
    }
  }

  async function register(): Promise<void> {
    if (!email || !password) { error = "Email and password are required."; return; }
    loading = true; error = "";
    try {
      const res = await api.authRegister(email, password, profileName || undefined);

      if ("confirmation_required" in res) {
        pendingEmail = email;
        pendingPassword = password;
        pendingProfileName = profileName;
        otpCode = "";
        error = "";
        loading = false;
        view = "register-otp";
        return;
      }

      auth.setSession(res.access_token, email, [res.profile], res.profile);
      libraryChanged.update((n) => n + 1);
      onclose();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function confirmRegistration(): Promise<void> {
    if (loading || !otpCode || otpCode.length < 8) return;
    loading = true; error = "";
    try {
      const res = await api.authConfirmRegister(
        pendingEmail,
        otpCode,
        pendingPassword,
        pendingProfileName || undefined,
      );
      auth.setSession(res.access_token, pendingEmail, [res.profile], res.profile, res.refresh_token);
      libraryChanged.update((n) => n + 1);
      view = "success";
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      otpCode = "";
    } finally {
      loading = false;
    }
  }
</script>

<!-- Backdrop -->
<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div
  class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
  onclick={(e) => { if (e.target === e.currentTarget) onclose(); }}
>
  <div class="relative w-full max-w-sm rounded-xl border border-border bg-background p-6 shadow-2xl">
    {#if view !== "success"}
      <button
        class="absolute right-4 top-4 text-muted-foreground hover:text-foreground"
        onclick={onclose}
      >
        <X class="size-4" />
      </button>
    {/if}

    {#if view === "choose"}
      <h2 class="mb-1 text-lg font-semibold">Sign in to Cove</h2>
      <p class="mb-6 text-sm text-muted-foreground">Sync your library across devices</p>
      <div class="flex flex-col gap-3">
        <Button variant="default" class="w-full" onclick={() => setView("login")}>
          Sign in with password
        </Button>
        <Button variant="outline" class="w-full" onclick={() => setView("otp-email")}>
          Sign in with email code
        </Button>
        <div class="relative my-1 flex items-center">
          <div class="flex-1 border-t border-border"></div>
          <span class="mx-3 text-xs text-muted-foreground">or</span>
          <div class="flex-1 border-t border-border"></div>
        </div>
        <Button variant="ghost" class="w-full" onclick={() => setView("register")}>
          Create account
        </Button>
      </div>

    {:else if view === "login"}
      <Button variant="link" class="mb-3 h-auto p-0 text-xs text-muted-foreground" onclick={() => setView("choose")}>
        ← Back
      </Button>
      <h2 class="mb-5 text-lg font-semibold">Sign in</h2>
      <div class="flex flex-col gap-3">
        <div class="relative">
          <Mail class="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="email"
            placeholder="Email"
            class="pl-9"
            bind:value={email}
            onkeydown={(e) => e.key === "Enter" && login()}
          />
        </div>
        <div class="relative">
          <Lock class="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="password"
            placeholder="Password"
            class="pl-9"
            bind:value={password}
            onkeydown={(e) => e.key === "Enter" && login()}
          />
        </div>
        {#if error}<p class="text-xs text-destructive">{error}</p>{/if}
        <Button class="w-full" onclick={login} disabled={loading}>
          {#if loading}<Spinner class="mr-2 size-4" />{/if}
          Sign in
        </Button>
        <Button variant="link" class="h-auto p-0 text-xs text-muted-foreground" onclick={() => setView("otp-email")}>
          Sign in with email code instead
        </Button>
      </div>

    {:else if view === "otp-email"}
      <Button variant="link" class="mb-3 h-auto p-0 text-xs text-muted-foreground" onclick={() => setView("choose")}>
        ← Back
      </Button>
      <h2 class="mb-1 text-lg font-semibold">Sign in with email code</h2>
      <p class="mb-5 text-sm text-muted-foreground">We'll send a one-time code to your inbox.</p>
      <div class="flex flex-col gap-3">
        <div class="relative">
          <Mail class="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="email"
            placeholder="Email"
            class="pl-9"
            bind:value={email}
            onkeydown={(e) => e.key === "Enter" && sendOTP()}
          />
        </div>
        {#if error}<p class="text-xs text-destructive">{error}</p>{/if}
        <Button class="w-full" onclick={sendOTP} disabled={loading}>
          {#if loading}<Spinner class="mr-2 size-4" />{/if}
          Send code
        </Button>
      </div>

    {:else if view === "otp-code"}
      <h2 class="mb-1 text-lg font-semibold">Enter your code</h2>
      <p class="mb-6 text-sm text-muted-foreground">
        Check your inbox at <strong>{otpEmail}</strong>
      </p>
      <div class="flex flex-col items-center gap-4">
        <InputOTP maxlength={8} bind:value={otpCode} onComplete={verifyOTP}>
          {#snippet children({ cells })}
            <InputOTPGroup>
              <InputOTPSlot cell={cells[0]} />
              <InputOTPSlot cell={cells[1]} />
              <InputOTPSlot cell={cells[2]} />
              <InputOTPSlot cell={cells[3]} />
            </InputOTPGroup>
            <InputOTPSeparator />
            <InputOTPGroup>
              <InputOTPSlot cell={cells[4]} />
              <InputOTPSlot cell={cells[5]} />
              <InputOTPSlot cell={cells[6]} />
              <InputOTPSlot cell={cells[7]} />
            </InputOTPGroup>
          {/snippet}
        </InputOTP>
        {#if error}<p class="text-xs text-destructive text-center">{error}</p>{/if}
        {#if loading}
          <div class="flex items-center gap-2 text-sm text-muted-foreground">
            <Spinner class="size-4" /> Verifying…
          </div>
        {:else}
          <Button class="w-full" onclick={verifyOTP} disabled={otpCode.length < 8}>
            Verify
          </Button>
        {/if}
        <Button variant="link" class="h-auto p-0 text-xs text-muted-foreground" onclick={() => { email = otpEmail; setView("otp-email"); }}>
          Resend code
        </Button>
      </div>

    {:else if view === "register"}
      <Button variant="link" class="mb-3 h-auto p-0 text-xs text-muted-foreground" onclick={() => setView("choose")}>
        ← Back
      </Button>
      <h2 class="mb-5 text-lg font-semibold">Create account</h2>
      <div class="flex flex-col gap-3">
        <div class="relative">
          <User class="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Display name (optional)"
            class="pl-9"
            bind:value={profileName}
          />
        </div>
        <div class="relative">
          <Mail class="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="email"
            placeholder="Email"
            class="pl-9"
            bind:value={email}
          />
        </div>
        <div class="relative">
          <Lock class="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="password"
            placeholder="Password"
            class="pl-9"
            bind:value={password}
            onkeydown={(e) => e.key === "Enter" && register()}
          />
        </div>
        {#if error}<p class="text-xs text-destructive">{error}</p>{/if}
        <Button class="w-full" onclick={register} disabled={loading}>
          {#if loading}<Spinner class="mr-2 size-4" />{/if}
          Create account
        </Button>
        <p class="text-xs text-muted-foreground">
          Your existing library will be synced to the new account.
        </p>
      </div>

    {:else if view === "register-otp"}
      <h2 class="mb-1 text-lg font-semibold">Check your email</h2>
      <p class="mb-2 text-sm text-muted-foreground">We sent a confirmation code to</p>
      <p class="mb-6 text-sm font-medium">{pendingEmail}</p>
      <div class="flex flex-col items-center gap-4">
        <InputOTP maxlength={8} bind:value={otpCode} onComplete={confirmRegistration}>
          {#snippet children({ cells })}
            <InputOTPGroup>
              <InputOTPSlot cell={cells[0]} />
              <InputOTPSlot cell={cells[1]} />
              <InputOTPSlot cell={cells[2]} />
              <InputOTPSlot cell={cells[3]} />
            </InputOTPGroup>
            <InputOTPSeparator />
            <InputOTPGroup>
              <InputOTPSlot cell={cells[4]} />
              <InputOTPSlot cell={cells[5]} />
              <InputOTPSlot cell={cells[6]} />
              <InputOTPSlot cell={cells[7]} />
            </InputOTPGroup>
          {/snippet}
        </InputOTP>
        {#if error}<p class="text-xs text-destructive text-center">{error}</p>{/if}
        {#if loading}
          <div class="flex items-center gap-2 text-sm text-muted-foreground">
            <Spinner class="size-4" /> Verifying…
          </div>
        {:else}
          <Button class="w-full" onclick={confirmRegistration} disabled={otpCode.length < 8}>
            Confirm account
          </Button>
        {/if}
        <Button
          variant="link"
          class="h-auto p-0 text-xs text-muted-foreground"
          onclick={() => { email = pendingEmail; profileName = pendingProfileName; setView("register"); }}
        >
          ← Back to registration
        </Button>
      </div>

    {:else if view === "success"}
      <div class="flex flex-col items-center gap-4 py-4 text-center">
        <CheckCircle class="size-12 text-green-500" />
        <div>
          <h2 class="text-lg font-semibold">Welcome to Cove!</h2>
          <p class="mt-1 text-sm text-muted-foreground">Your account is ready and your library has been synced.</p>
        </div>
        <Button class="w-full" onclick={onclose}>Get started</Button>
      </div>
    {/if}
  </div>
</div>
