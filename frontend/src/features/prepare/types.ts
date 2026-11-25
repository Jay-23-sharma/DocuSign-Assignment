import type { Rect } from "../../components/pdf/fieldGeometry";

export type Recipient = { id: number; name: string; email: string; status?: string;
  delivery_status?: "accepted" | "failed" | "unconfirmed" | null; };
export type Field = Rect & {
  recipient_id: number;
  field_type: string;
  page_number: number;
  required: boolean;
};
export type Box = Field & { key: string };
export type PrepareDetail = {
  completed_sha256?: string | null;
  email_subject: string | null;
  email_message: string | null;
  expires_at: string | null;
  title: string;
  status: string;
  original: null | { page_count: number };
  recipients: Recipient[];
  fields: Field[];
};
