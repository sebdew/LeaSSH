import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from "react";

export function Button({ variant = "primary", className = "", ...p }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" | "ghost" }) {
  const v = {
    primary: "bg-primary text-primary-fg hover:opacity-90",
    secondary: "bg-surface-2 text-fg border border-border hover:bg-border",
    ghost: "text-muted hover:bg-surface-2 hover:text-fg",
  }[variant];
  return <button {...p} className={`inline-flex h-9 items-center justify-center gap-2 rounded-field px-4 font-medium disabled:opacity-50 ${v} ${className}`} />;
}

export function Field({ label, id, ...p }: InputHTMLAttributes<HTMLInputElement> & { label: string; id: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="font-medium">{label}</label>
      <input id={id} {...p} className="h-9 rounded-field border border-border-strong bg-surface px-3 text-fg disabled:cursor-not-allowed disabled:bg-surface-2 disabled:text-muted" />
    </div>
  );
}

export function Card({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`rounded-card border border-border bg-surface p-6 shadow-[var(--shadow)] ${className}`}>{children}</div>;
}

export function ErrorBox({ message, detail, title }: { message: string; detail?: string; title: string }) {
  return (
    <div role="alert" className="rounded-field border border-destructive bg-surface-2 p-3 text-destructive">
      <p>{message}</p>
      {detail && (
        <details className="mt-1 text-muted">
          <summary className="cursor-pointer">{title}</summary>
          <code className="font-mono text-[13px]">{detail}</code>
        </details>
      )}
    </div>
  );
}
