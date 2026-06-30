// CivicSync Flutter Android application entry point.
//
// Startup sequence:
//   1. WidgetsFlutterBinding.ensureInitialized() — required before any async
//      operations (including Firebase.initializeApp).
//   2. Firebase.initializeApp() — initialises all Firebase services:
//        firebase_auth, cloud_firestore, firebase_storage.
//   3. GoogleSignIn.instance.initialize() — required once before any Google
//      Sign-In calls (google_sign_in 7.x singleton API).
//   4. Run the MaterialApp with a role-aware routing stub that hands off to
//      AuthScreen while the user is unauthenticated.
//
// Requirements: 1.1 (Google OAuth), 2.1 (camera capture), 7.1 (map feed).

import 'package:firebase_core/firebase_core.dart';
import 'package:flutter/material.dart';
import 'package:google_sign_in/google_sign_in.dart';

import 'screens/auth_screen.dart';
import 'screens/citizen_feed_screen.dart';
import 'screens/confirm_screen.dart';
import 'screens/my_issues_screen.dart';
import 'screens/official_dashboard_screen.dart';
import 'screens/profile_screen.dart';
import 'screens/report_flow.dart';

Future<void> main() async {
  // Must be called before any plugin or async code.
  WidgetsFlutterBinding.ensureInitialized();

  await Firebase.initializeApp();
  // google_sign_in 6.x does not require explicit initialization.

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
        '/report': (_) => const ReportFlowScreen(),
        '/triage-confirm': (_) => const ConfirmScreen(),
        // CitizenFeedScreen — implemented in task 16.
        '/citizen-feed': (_) => const CitizenFeedScreen(),
        // OfficialDashboardScreen — Kanban management view (task 17).
        '/official-dash': (_) => const OfficialDashboardScreen(),
        // ProfileScreen — user info and City Official Access (task 18).
        '/profile': (_) => const ProfileScreen(),
        // MyIssuesScreen — issues reported by the current user.
        '/my-issues': (_) => const MyIssuesScreen(),
      },
    );
  }
}

