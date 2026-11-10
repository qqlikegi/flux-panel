import { Navbar } from "@/components/navbar";

export default function DefaultLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="fz-canvas relative flex flex-col min-h-screen">
      <Navbar />
      <main className="relative z-[1] container mx-auto max-w-7xl px-4 sm:px-6 flex-grow pt-4 sm:pt-10">
        {children}
      </main>
    </div>
  );
}
