import type { Profile, AuthSession } from "$lib/types/auth";
import { supabase } from "$lib/supabase";

class AuthStore {
  session = $state<AuthSession | null>(null);
  profiles = $state<Profile[]>([]);
  activeProfile = $state<Profile | null>(null);

  // Private: JWT for injection into API requests.
  #token = $state<string | null>(null);

  get isGuest(): boolean {
    return this.session === null;
  }

  get authToken(): string | null {
    return this.#token;
  }

  async init(api: {
    profilesList: () => Promise<{ profiles: Profile[]; active_profile_id: string }>;
  }): Promise<void> {
    try {
      const data = await api.profilesList();
      this.profiles = data.profiles;
      this.activeProfile =
        data.profiles.find((p) => p.id === data.active_profile_id) ??
        data.profiles[0] ??
        null;
    } catch (e) {
      console.error("auth init: load profiles:", e);
    }

    if (!supabase) return;

    const { data } = await supabase.auth.getSession();
    if (data.session) {
      this.#token = data.session.access_token;
      this.session = {
        accessToken: data.session.access_token,
        email: data.session.user.email ?? "",
      };
    }

    supabase.auth.onAuthStateChange((_event, s) => {
      if (s) {
        this.#token = s.access_token;
        this.session = { accessToken: s.access_token, email: s.user.email ?? "" };
      } else {
        this.#token = null;
        this.session = null;
      }
    });
  }

  setSession(
    accessToken: string,
    email: string,
    profs: Profile[],
    active: Profile,
    refreshToken?: string,
  ): void {
    this.#token = accessToken;
    this.session = { accessToken, email };
    this.profiles = profs;
    this.activeProfile = active;
    // Persist through the Supabase JS client so getSession() restores on restart
    // and the client handles token auto-refresh.
    if (supabase && refreshToken) {
      supabase.auth.setSession({ access_token: accessToken, refresh_token: refreshToken });
    }
  }

  setProfiles(profs: Profile[], active: Profile): void {
    this.profiles = profs;
    this.activeProfile = active;
  }

  async logout(): Promise<void> {
    this.#token = null;
    this.session = null;
    if (supabase) await supabase.auth.signOut();
  }
}

export const auth = new AuthStore();
