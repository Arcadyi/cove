export interface Profile {
  id: string;
  name: string;
  is_primary: boolean;
  supabase_uid: string | null;
}

export interface AuthSession {
  accessToken: string;
  email: string;
}
