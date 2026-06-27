// CivicSync Flutter Android application entry point.
//
// Startup sequence:
//   1. WidgetsFlutterBinding.ensureInitialized() — required before any async
//      operations (including Firebase.initializeApp).
//   2. Firebase.initializeApp() — initialises all Firebase services:
//        firebase_auth, cloud_firestore, firebase_storage.
//   3. Run the MaterialApp with a role-aware routing stub that hands off to
//      AuthScreen while the user is unauthenticated.
//
// Requirements: 1.1 (Google OAuth), 2.1 (camera capture), 7.1 (map feed).

import 'package:firebase_core/firebase_core.dart';
import 'package:flutter/material.dart';

import 'screens/auth_screen.dart';

Future<void> main() async {
  // Must be called before any plugin or async code.
  WidgetsFlutterBinding.ensureInitialized();

  // Initialise Firebase using the configuration in google-services.json.
  // TODO: Replace the default FirebaseOptions with the real values from your
  //       Firebase project once google-services.json is configured.
  await Firebase.initializeApp();

  runApp(const CivicSyncApp());
}

/// Root widget for the CivicSync application.
class CivicSyncApp extends StatelessWidget {
  const CivicSyncApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'CivicSync',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFF1A73E8), // Google Blue
        ),
        useMaterial3: true,
      ),
      // Initial route: always start at the auth screen.
      // AuthScreen decides whether to show the sign-in UI or navigate
      // to CitizenFeedScreen / OfficialDashboardScreen based on the
      // current Firebase Auth state and the user's Firestore role.
      initialRoute: '/auth',
      routes: {
        '/auth': (_) => const AuthScreen(),
        // Subsequent screens are registered here as they are implemented:
        // '/citizen-feed'  : (_) => const CitizenFeedScreen(),
        // '/official-dash' : (_) => const OfficialDashboardScreen(),
        // '/report'        : (_) => const ReportFlowScreen(),
      },
    );
  }
}
