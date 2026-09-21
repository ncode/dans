import type { ReactNode } from "react";
import Alert from "@cloudscape-design/components/alert";
import Button from "@cloudscape-design/components/button";
import FormField from "@cloudscape-design/components/form-field";
import Input from "@cloudscape-design/components/input";
import Select from "@cloudscape-design/components/select";
import SpaceBetween from "@cloudscape-design/components/space-between";
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
export function Failure({
  error,
  onRetry,
}: {
  error: string;
  onRetry?: () => void;
}) {
  return error ? (
    <Alert
      type="error"
      action={onRetry && <Button onClick={onRetry}>Retry loading</Button>}
    >
      {error}
    </Alert>
  ) : null;
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
export const messageOf = (error: unknown) =>
  error instanceof Error
    ? error.message
    : "The request could not be completed.";
