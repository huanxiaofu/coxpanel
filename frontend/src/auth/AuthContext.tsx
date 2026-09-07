import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { api } from '../api';
import type { AuthUser } from '../api';

interface AuthContextValue {
  user: AuthUser | null;
  loading: boolean;
  error: string | null;
  establishSession: (sessionToken: string) => void;
  signOut: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [sessionToken, setSessionToken] = useState(() => localStorage.getItem('coxpanel_token') || '');
  const [user, setUser] = useState<AuthUser | null>(null);
  const [loading, setLoading] = useState(Boolean(sessionToken));
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    if (!sessionToken) {
      setUser(null);
      setError(null);
      setLoading(false);
      return () => {
        active = false;
      };
    }

    setLoading(true);
    setError(null);
    api.me()
      .then((currentUser) => {
        if (active) setUser(currentUser);
      })
      .catch((reason: unknown) => {
        if (active) {
          setUser(null);
          setError(reason instanceof Error ? reason.message : '当前用户信息读取失败');
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });

    return () => {
      active = false;
    };
  }, [sessionToken]);

  const value = useMemo<AuthContextValue>(() => ({
    user,
    loading,
    error,
    establishSession: (nextToken: string) => {
      localStorage.setItem('coxpanel_token', nextToken);
      localStorage.removeItem('coxpanel_user');
      setLoading(true);
      setError(null);
      setSessionToken(nextToken);
    },
    signOut: () => {
      localStorage.removeItem('coxpanel_token');
      localStorage.removeItem('coxpanel_user');
      setSessionToken('');
      setUser(null);
      setError(null);
    },
  }), [error, loading, user]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext);
  if (!context) throw new Error('useAuth must be used inside AuthProvider');
  return context;
}

export function isAdminRole(role: string | null | undefined): boolean {
  return role === 'admin' || role === 'owner';
}
