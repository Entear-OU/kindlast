import type { Metadata } from "next";
import { Analytics } from "@vercel/analytics/next";
import { Montserrat, Geist_Mono } from "next/font/google";
import { Toaster } from "@/components/ui/sonner";
import "./globals.css";

const montserrat = Montserrat({
  variable: "--font-sans",
  subsets: ["latin"],
  weight: ["400", "500", "600", "700", "800", "900"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Kindlast: AI-Powered GDPR & AI Act Compliance",
  description:
    "Two regulations, one workflow. Continuous GDPR and EU AI Act compliance for any company building in Europe.",
  openGraph: {
    title: "Kindlast: AI-Powered GDPR & AI Act Compliance",
    description:
      "Two regulations, one workflow. Continuous GDPR and EU AI Act compliance for any company building in Europe.",
    type: "website",
    locale: "en_US",
    siteName: "Kindlast",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body
        className={`${montserrat.variable} ${geistMono.variable} antialiased`}
      >
        {children}
        <Toaster />
        <Analytics />
      </body>
    </html>
  );
}
