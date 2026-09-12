import type { ReactNode } from "react";
import Alert from "@cloudscape-design/components/alert";
import Button from "@cloudscape-design/components/button";
import FormField from "@cloudscape-design/components/form-field";
import Input from "@cloudscape-design/components/input";
import Select from "@cloudscape-design/components/select";
import SpaceBetween from "@cloudscape-design/components/space-between";
import { request } from "./api.ts";
import type { Page } from "./domain.ts";
export function Field({
  label,
  value,
  onChange,
  type = "text",
  disabled = false,
  description,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  type?: "text" | "number" | "password";
  disabled?: boolean;
  description?: ReactNode;
}) {
  return (
    <FormField label={label} description={description}>
      <Input
        value={value}
        onChange={({ detail }) => onChange(detail.value)}
        type={type}
        disabled={disabled}
        autoComplete={type === "password" ? "current-password" : "off"}
      />
    </FormField>
  );
}
export function Choice({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}) {
  return (
    <FormField label={label}>
      <Select
        selectedOption={
          options.find((option) => option.value === value) ?? null
        }
        options={options}
        filteringType="auto"
        onChange={({ detail }) => onChange(detail.selectedOption.value ?? "")}
        empty="No options"
      />
    </FormField>
  );
}
export function Failure({ error }: { error: string }) {
  return error ? <Alert type="error">{error}</Alert> : null;
}
export function Pager({
  previous,
  next,
  busy,
  onPrevious,
  onNext,
}: {
  previous: boolean;
  next: boolean;
  busy: boolean;
  onPrevious: () => void;
  onNext: () => void;
}) {
  return (
    <SpaceBetween direction="horizontal" size="xs">
      <Button disabled={!previous || busy} onClick={onPrevious}>
        Previous
      </Button>
      <Button disabled={!next || busy} onClick={onNext}>
        Next
      </Button>
    </SpaceBetween>
  );
}
export async function allPages<T>(path: string): Promise<T[]> {
  const items: T[] = [];
  let cursor: string | null = null;
  const seen = new Set<string>();
  do {
    const page: Page<T> = await request(
      path +
        (path.includes("?") ? "&" : "?") +
        new URLSearchParams({ limit: "100", ...(cursor ? { cursor } : {}) }),
    );
    items.push(...page.items);
    cursor = page.next_cursor;
    if (cursor && seen.has(cursor))
      throw new Error("The API repeated a page. Reload to try again.");
    if (cursor) seen.add(cursor);
  } while (cursor);
  return items;
}
export const messageOf = (error: unknown) =>
  error instanceof Error
    ? error.message
    : "The request could not be completed.";
